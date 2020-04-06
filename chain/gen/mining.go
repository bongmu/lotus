package gen

import (
	"context"

	bls "github.com/filecoin-project/filecoin-ffi"
	amt "github.com/filecoin-project/go-amt-ipld/v2"
	"github.com/filecoin-project/specs-actors/actors/abi"
	"github.com/filecoin-project/specs-actors/actors/crypto"
	cid "github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/chain/wallet"
)

func MinerCreateBlock(ctx context.Context, sm *stmgr.StateManager, w *wallet.Wallet, miner address.Address,
	parents *types.TipSet, ticket *types.Ticket, proof *types.EPostProof, bvals []types.BeaconEntry,
	msgs []*types.SignedMessage, height abi.ChainEpoch, timestamp uint64) (*types.FullBlock, error) {
	st, recpts, err := sm.TipSetState(ctx, parents)
	if err != nil {
		return nil, xerrors.Errorf("failed to load tipset state: %w", err)
	}

	worker, err := stmgr.GetMinerWorkerRaw(ctx, sm, st, miner)
	if err != nil {
		return nil, xerrors.Errorf("failed to get miner worker: %w", err)
	}

	next := &types.BlockHeader{
		Miner:                 miner,
		Parents:               parents.Cids(),
		Ticket:                ticket,
		BeaconEntries:         bvals,
		Height:                height,
		Timestamp:             timestamp,
		EPostProof:            *proof,
		ParentStateRoot:       st,
		ParentMessageReceipts: recpts,
	}

	var blsMessages []*types.Message
	var secpkMessages []*types.SignedMessage

	var blsMsgCids, secpkMsgCids []cid.Cid
	var blsSigs []crypto.Signature
	for _, msg := range msgs {
		if msg.Signature.Type == crypto.SigTypeBLS {
			blsSigs = append(blsSigs, msg.Signature)
			blsMessages = append(blsMessages, &msg.Message)

			c, err := sm.ChainStore().PutMessage(&msg.Message)
			if err != nil {
				return nil, err
			}

			blsMsgCids = append(blsMsgCids, c)
		} else {
			c, err := sm.ChainStore().PutMessage(msg)
			if err != nil {
				return nil, err
			}

			secpkMsgCids = append(secpkMsgCids, c)
			secpkMessages = append(secpkMessages, msg)

		}
	}

	bs := cbor.NewCborStore(sm.ChainStore().Blockstore())
	blsmsgroot, err := amt.FromArray(ctx, bs, toIfArr(blsMsgCids))
	if err != nil {
		return nil, xerrors.Errorf("building bls amt: %w", err)
	}
	secpkmsgroot, err := amt.FromArray(ctx, bs, toIfArr(secpkMsgCids))
	if err != nil {
		return nil, xerrors.Errorf("building secpk amt: %w", err)
	}

	mmcid, err := bs.Put(ctx, &types.MsgMeta{
		BlsMessages:   blsmsgroot,
		SecpkMessages: secpkmsgroot,
	})
	if err != nil {
		return nil, err
	}
	next.Messages = mmcid

	aggSig, err := aggregateSignatures(blsSigs)
	if err != nil {
		return nil, err
	}

	next.BLSAggregate = aggSig
	pweight, err := sm.ChainStore().Weight(ctx, parents)
	if err != nil {
		return nil, err
	}
	next.ParentWeight = pweight

	cst := cbor.NewCborStore(sm.ChainStore().Blockstore())
	tree, err := state.LoadStateTree(cst, st)
	if err != nil {
		return nil, xerrors.Errorf("failed to load state tree: %w", err)
	}

	waddr, err := vm.ResolveToKeyAddr(tree, cst, worker)
	if err != nil {
		return nil, xerrors.Errorf("failed to resolve miner address to key address: %w", err)
	}

	nosigbytes, err := next.SigningBytes()
	if err != nil {
		return nil, xerrors.Errorf("failed to get signing bytes for block: %w", err)
	}

	sig, err := w.Sign(ctx, waddr, nosigbytes)
	if err != nil {
		return nil, xerrors.Errorf("failed to sign new block: %w", err)
	}

	next.BlockSig = sig

	fullBlock := &types.FullBlock{
		Header:        next,
		BlsMessages:   blsMessages,
		SecpkMessages: secpkMessages,
	}

	return fullBlock, nil
}

func aggregateSignatures(sigs []crypto.Signature) (*crypto.Signature, error) {
	var blsSigs []bls.Signature
	for _, s := range sigs {
		var bsig bls.Signature
		copy(bsig[:], s.Data)
		blsSigs = append(blsSigs, bsig)
	}

	aggSig := bls.Aggregate(blsSigs)
	return &crypto.Signature{
		Type: crypto.SigTypeBLS,
		Data: aggSig[:],
	}, nil
}

func toIfArr(cids []cid.Cid) []cbg.CBORMarshaler {
	out := make([]cbg.CBORMarshaler, 0, len(cids))
	for _, c := range cids {
		oc := cbg.CborCid(c)
		out = append(out, &oc)
	}
	return out
}
