package advmgr

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gorilla/mux"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-sectorbuilder"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/lib/tarutil"

	"github.com/filecoin-project/specs-actors/actors/abi"

	"github.com/filecoin-project/lotus/node/config"
)

const metaFile = "sectorstore.json"

var pathTypes = []sectorbuilder.SectorFileType{sectorbuilder.FTUnsealed, sectorbuilder.FTSealed, sectorbuilder.FTCache}

type storage struct {
	localLk      sync.RWMutex
	localStorage LocalStorage

	paths []*path
}

type path struct {
	lk sync.Mutex

	meta  config.StorageMeta
	local string

	sectors map[abi.SectorID]sectorbuilder.SectorFileType
}

func (st *storage) openPath(p string) error {
	mb, err := ioutil.ReadFile(filepath.Join(p, metaFile))
	if err != nil {
		return xerrors.Errorf("reading storage metadata for %s: %w", p, err)
	}

	var meta config.StorageMeta
	if err := json.Unmarshal(mb, &meta); err != nil {
		return xerrors.Errorf("unmarshalling storage metadata for %s: %w", p, err)
	}

	// TODO: Check existing / dedupe

	out := &path{
		meta:    meta,
		local:   p,
		sectors: map[abi.SectorID]sectorbuilder.SectorFileType{},
	}

	for _, t := range pathTypes {
		ents, err := ioutil.ReadDir(filepath.Join(p, t.String()))
		if err != nil {
			if os.IsNotExist(err) {
				if err := os.MkdirAll(filepath.Join(p, t.String()), 0755); err != nil {
					return xerrors.Errorf("openPath mkdir '%s': %w", filepath.Join(p, t.String()), err)
				}

				continue
			}
			return xerrors.Errorf("listing %s: %w", filepath.Join(p, t.String()), err)
		}

		for _, ent := range ents {
			sid, err := parseSectorID(ent.Name())
			if err != nil {
				return xerrors.Errorf("parse sector id %s: %w", ent.Name(), err)
			}

			out.sectors[sid] |= t
		}
	}

	st.paths = append(st.paths, out)

	return nil
}

func (st *storage) open() error {
	st.localLk.Lock()
	defer st.localLk.Unlock()

	cfg, err := st.localStorage.GetStorage()
	if err != nil {
		return xerrors.Errorf("getting local storage config: %w", err)
	}

	for _, path := range cfg.StoragePaths {
		err := st.openPath(path.Path)
		if err != nil {
			return xerrors.Errorf("opening path %s: %w", path.Path, err)
		}
	}

	return nil
}

func (st *storage) acquireSector(mid abi.ActorID, id abi.SectorNumber, existing sectorbuilder.SectorFileType, allocate sectorbuilder.SectorFileType, sealing bool) (sectorbuilder.SectorPaths, func(), error) {
	if existing|allocate != existing^allocate {
		return sectorbuilder.SectorPaths{}, nil, xerrors.New("can't both find and allocate a sector")
	}

	st.localLk.RLock()

	var out sectorbuilder.SectorPaths

	for _, fileType := range pathTypes {
		if fileType&existing == 0 {
			continue
		}

		for _, p := range st.paths {
			p.lk.Lock()
			s, ok := p.sectors[abi.SectorID{
				Miner:  mid,
				Number: id,
			}]
			p.lk.Unlock()
			if !ok {
				continue
			}
			if s&fileType == 0 {
				continue
			}
			if p.local == "" {
				continue // TODO: fetch
			}

			spath := filepath.Join(p.local, fileType.String(), fmt.Sprintf("s-t0%d-%d", mid, id))

			switch fileType {
			case sectorbuilder.FTUnsealed:
				out.Unsealed = spath
			case sectorbuilder.FTSealed:
				out.Sealed = spath
			case sectorbuilder.FTCache:
				out.Cache = spath
			}

			existing ^= fileType
		}
	}

	for _, fileType := range pathTypes {
		if fileType&allocate == 0 {
			continue
		}

		var best string

		for _, p := range st.paths {
			if sealing && !p.meta.CanSeal {
				continue
			}
			if !sealing && !p.meta.CanStore {
				continue
			}

			p.lk.Lock()
			p.sectors[abi.SectorID{
				Miner:  mid,
				Number: id,
			}] |= fileType
			p.lk.Unlock()

			// TODO: Check free space
			// TODO: Calc weights

			best = filepath.Join(p.local, fileType.String(), fmt.Sprintf("s-t0%d-%d", mid, id))
			break // todo: the first path won't always be the best
		}

		if best == "" {
			st.localLk.RUnlock()
			return sectorbuilder.SectorPaths{}, nil, xerrors.Errorf("couldn't find a suitable path for a sector")
		}

		switch fileType {
		case sectorbuilder.FTUnsealed:
			out.Unsealed = best
		case sectorbuilder.FTSealed:
			out.Sealed = best
		case sectorbuilder.FTCache:
			out.Cache = best
		}

		allocate ^= fileType
	}

	return out, st.localLk.RUnlock, nil
}

func (st *storage) findBestAllocStorage(allocate sectorbuilder.SectorFileType, sealing bool) ([]config.StorageMeta, error) {
	var out []config.StorageMeta

	for _, p := range st.paths {
		if sealing && !p.meta.CanSeal {
			continue
		}
		if !sealing && !p.meta.CanStore {
			continue
		}

		// TODO: filter out of space

		out = append(out, p.meta)
	}

	if len(out) == 0 {
		return nil, xerrors.New("no good path found")
	}

	// todo: sort by some kind of preference
	return out, nil
}

func (st *storage) findSector(mid abi.ActorID, sn abi.SectorNumber, typ sectorbuilder.SectorFileType) ([]config.StorageMeta, error) {
	var out []config.StorageMeta
	for _, p := range st.paths {
		p.lk.Lock()
		t := p.sectors[abi.SectorID{
			Miner:  mid,
			Number: sn,
		}]
		if t|typ == 0 {
			continue
		}
		p.lk.Unlock()
		out = append(out, p.meta)
	}
	if len(out) == 0 {
		return nil, xerrors.Errorf("sector %s/s-t0%d-%d not found", typ, mid, sn)
	}

	return out, nil
}

func (st *storage) local() []api.StoragePath {
	var out []api.StoragePath
	for _, p := range st.paths {
		if p.local == "" {
			continue
		}

		out = append(out, api.StoragePath{
			ID:        p.meta.ID,
			Weight:    p.meta.Weight,
			LocalPath: p.local,
			CanSeal:   p.meta.CanSeal,
			CanStore:  p.meta.CanStore,
		})
	}

	return out
}

func (st *storage) ServeHTTP(w http.ResponseWriter, r *http.Request) { // /storage/
	mux := mux.NewRouter()

	mux.HandleFunc("/{type}/{id}", st.remoteGetSector).Methods("GET")

	log.Infof("SERVEGETREMOTE %s", r.URL)

	mux.ServeHTTP(w, r)
}


func (st *storage) remoteGetSector(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id, err := parseSectorID(vars["id"])
	if err != nil {
		log.Error(err)
		w.WriteHeader(500)
		return
	}

	ft, err := ftFromString(vars["type"])
	if err != nil {
		return
	}
	paths, done, err := st.acquireSector(id.Miner, id.Number, ft, 0, false)
	if err != nil {
		return
	}
	defer done()

	var path string
	switch ft {
	case sectorbuilder.FTUnsealed:
		path = paths.Unsealed
	case sectorbuilder.FTSealed:
		path = paths.Sealed
	case sectorbuilder.FTCache:
		path = paths.Cache
	}
	if path == "" {
		log.Error("acquired path was empty")
		w.WriteHeader(500)
		return
	}

	stat, err := os.Stat(path)
	if err != nil {
		log.Error(err)
		w.WriteHeader(500)
		return
	}

	var rd io.Reader
	if stat.IsDir() {
		rd, err = tarutil.TarDirectory(path)
		w.Header().Set("Content-Type", "application/x-tar")
	} else {
		rd, err = os.OpenFile(path, os.O_RDONLY, 0644)
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if err != nil {
		log.Error(err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(200)
	if _, err := io.Copy(w, rd); err != nil { // TODO: default 32k buf may be too small
		log.Error(err)
		return
	}
}

func ftFromString(t string) (sectorbuilder.SectorFileType, error) {
	switch t {
	case sectorbuilder.FTUnsealed.String():
		return sectorbuilder.FTUnsealed, nil
	case sectorbuilder.FTSealed.String():
		return sectorbuilder.FTSealed, nil
	case sectorbuilder.FTCache.String():
		return sectorbuilder.FTCache, nil
	default:
		return 0, xerrors.Errorf("unknown sector file type: '%s'", t)
	}
}

func parseSectorID(baseName string) (abi.SectorID, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
	if err != nil {
		return abi.SectorID{}, xerrors.Errorf(": %w", err)
	}

	if read != 2 {
		return abi.SectorID{}, xerrors.Errorf("parseSectorID expected to scan 2 values, got %d", read)
	}

	return abi.SectorID{
		Miner:  mid,
		Number: n,
	}, nil
}
