package validation

// Errors extracted from `ValidateBlock` and `checkBlockMessages`.
// Explicitly split into `var` declarations instead of having a single block to avoid `go fmt`
// potentially changing alignment of all errors (producing harder-to-read diffs).
// FIXME: How to graphically express this error hierarchy *without* reflection? (nested packages?)

var ErrInvalidBlock = NewHierarchicalErrorClass("invalid block")

var ErrBlockNilSignature = ErrInvalidBlock.Child("block had nil signature")

var ErrTimestamp = ErrInvalidBlock.Child("block timestamp error")
var ErrBlockFutureTimestamp = ErrTimestamp.Child("ahead of current time")

var ErrBlockMinedEarly = ErrInvalidBlock.Child("block was generated too soon")

var ErrWinner = ErrInvalidBlock.Child("not a winner block")
var ErrSlashedMiner = ErrWinner.Child("slashed or invalid miner")
var ErrNoCandidates = ErrWinner.Child("no candidates")
var ErrDuplicateCandidates = ErrWinner.Child("duplicate epost candidates")

// FIXME: Might want to include these in some EPost category.

var ErrMiner = ErrInvalidBlock.Child("invalid miner")

var ErrInvalidMessagesCID = ErrInvalidBlock.Child("messages CID didn't match message root in header")

var ErrInvalidMessageInBlock = ErrInvalidBlock.Child("invalid message")
var ErrInvalidBlsSignature = ErrInvalidMessageInBlock.Child("invalid bls aggregate signature")
var ErrInvalidSecpkSignature = ErrInvalidMessageInBlock.Child("invalid secpk signature")
var ErrEmptyRecipient = ErrInvalidMessageInBlock.Child("empty 'To' address")
var ErrWrongNonce = ErrInvalidMessageInBlock.Child("wrong nonce")

// FIXME: Errors from `checkBlockMessages` are too generic and should probably be extracted, is there
//  another place where we validate them (like the message pool)? In that case we need a different
//  root error like `ErrInvalidMessage`.
