package cosipbft

import (
	"github.com/golang/protobuf/proto"
	"go.dedis.ch/fabric/consensus"
	"go.dedis.ch/fabric/crypto"
	"go.dedis.ch/fabric/encoding"
	"golang.org/x/xerrors"
)

// Prepare is the request sent at the beginning of the PBFT protocol.
type Prepare struct {
	proposal consensus.Proposal
	digest   []byte
}

// GetHash returns the hash of the prepare request that will be signed by the
// collective authority.
func (p Prepare) GetHash() []byte {
	return p.digest
}

// Pack returns the protobuf message, or an error.
func (p Prepare) Pack(enc encoding.ProtoMarshaler) (proto.Message, error) {
	pb := &PrepareRequest{}
	var err error
	pb.Proposal, err = enc.PackAny(p.proposal)
	if err != nil {
		return nil, xerrors.Errorf("couldn't pack proposal: %v", err)
	}

	return pb, nil
}

// Commit is the request sent for the last phase of the PBFT.
type Commit struct {
	to      Digest
	prepare crypto.Signature
	hash    []byte
}

func newCommitRequest(to []byte, prepare crypto.Signature) (Commit, error) {
	buffer, err := prepare.MarshalBinary()
	if err != nil {
		return Commit{}, xerrors.Errorf("couldn't marshal prepare signature: %v", err)
	}

	commit := Commit{
		to:      to,
		prepare: prepare,
		hash:    buffer,
	}

	return commit, nil
}

// GetHash returns the hash for the commit message. The actual value is the
// marshaled prepare signature.
func (c Commit) GetHash() []byte {
	return c.hash
}

// Pack returns the protobuf message representation of a commit, or an error if
// something goes wrong during encoding.
func (c Commit) Pack(enc encoding.ProtoMarshaler) (proto.Message, error) {
	pb := &CommitRequest{
		To: c.to,
	}

	var err error
	pb.Prepare, err = enc.PackAny(c.prepare)
	if err != nil {
		return nil, xerrors.Errorf("couldn't pack prepare signature: %v", err)
	}

	return pb, nil
}
