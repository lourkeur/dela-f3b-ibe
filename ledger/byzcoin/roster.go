package byzcoin

import (
	proto "github.com/golang/protobuf/proto"
	any "github.com/golang/protobuf/ptypes/any"
	"go.dedis.ch/fabric/consensus/viewchange"
	"go.dedis.ch/fabric/crypto"
	"go.dedis.ch/fabric/encoding"
	"go.dedis.ch/fabric/mino"
	"golang.org/x/xerrors"
)

// iterator is a generic implementation of an iterator over a list of conodes.
type iterator struct {
	index  int
	roster *roster
}

func (i *iterator) HasNext() bool {
	return i.index < i.roster.Len()
}

func (i *iterator) GetNext() int {
	res := i.index
	i.index++
	return res
}

// addressIterator is an iterator for a list of addresses.
//
// - implements mino.AddressIterator
type addressIterator struct {
	*iterator
}

// GetNext implements mino.AddressIterator. It returns the next address.
func (i *addressIterator) GetNext() mino.Address {
	if i.iterator.HasNext() {
		return i.roster.addrs[i.iterator.GetNext()]
	}
	return nil
}

// publicKeyIterator is an iterator for a list of public keys.
//
// - implements crypto.PublicKeyIterator
type publicKeyIterator struct {
	*iterator
}

// GetNext implements crypto.PublicKeyIterator. It returns the next public key.
func (i *publicKeyIterator) GetNext() crypto.PublicKey {
	if i.iterator.HasNext() {
		return i.roster.pubkeys[i.iterator.GetNext()]
	}
	return nil
}

// roster contains a list of participants with their addresses and public keys.
//
// - implements crypto.CollectiveAuthority
// - implements viewchange.EvolvableAuthority
// - implements mino.Players
// - implements encoding.Packable
type roster struct {
	addrs   []mino.Address
	pubkeys []crypto.PublicKey
}

// Take implements mino.Players. It returns a subset of the roster according to
// the filter.
func (r roster) Take(updaters ...mino.FilterUpdater) mino.Players {
	filter := mino.ApplyFilters(updaters)
	newRoster := roster{
		addrs:   make([]mino.Address, len(filter.Indices)),
		pubkeys: make([]crypto.PublicKey, len(filter.Indices)),
	}

	for i, k := range filter.Indices {
		newRoster.addrs[i] = r.addrs[k]
		newRoster.pubkeys[i] = r.pubkeys[k]
	}

	return newRoster
}

// Apply implements viewchange.EvolvableAuthority. It returns a new authority
// after applying the change set.
func (r roster) Apply(viewchange.ChangeSet) viewchange.EvolvableAuthority {
	// TODO: implement
	return r
}

// Len implements mino.Players. It returns the length of the roster.
func (r roster) Len() int {
	return len(r.addrs)
}

// GetPublicKey implements crypto.CollectiveAuthority. It returns the public key
// of the address if it exists, nil otherwise. The second return is the index of
// the public key in the roster.
func (r roster) GetPublicKey(target mino.Address) (crypto.PublicKey, int) {
	for i, addr := range r.addrs {
		if addr.Equal(target) {
			return r.pubkeys[i], i
		}
	}

	return nil, -1
}

// AddressIterator implements mino.Players. It returns an iterator of the
// addresses of the roster in a deterministic order.
func (r roster) AddressIterator() mino.AddressIterator {
	return &addressIterator{iterator: &iterator{roster: &r}}
}

// PublicKeyIterator implements crypto.CollectiveAuthority. It returns an
// iterator of the public keys of the roster in a deterministic order.
func (r roster) PublicKeyIterator() crypto.PublicKeyIterator {
	return &publicKeyIterator{iterator: &iterator{roster: &r}}
}

// Pack implements encoding.Packable. It returns the protobuf message for the
// roster.
func (r roster) Pack(enc encoding.ProtoMarshaler) (proto.Message, error) {
	addrs := make([][]byte, r.Len())
	pubkeys := make([]*any.Any, r.Len())

	var err error
	for i, addr := range r.addrs {
		addrs[i], err = addr.MarshalText()
		if err != nil {
			return nil, xerrors.Errorf("couldn't marshal address: %v", err)
		}

		pubkeys[i], err = enc.PackAny(r.pubkeys[i])
		if err != nil {
			return nil, xerrors.Errorf("couldn't pack public key: %v", err)
		}
	}

	pb := &Roster{
		Addresses:  addrs,
		PublicKeys: pubkeys,
	}

	return pb, nil
}

// rosterFactory provide functions to create and decode a roster.
type rosterFactory struct {
	addressFactory mino.AddressFactory
	pubkeyFactory  crypto.PublicKeyFactory
}

func newRosterFactory(af mino.AddressFactory, pf crypto.PublicKeyFactory) rosterFactory {
	return rosterFactory{
		addressFactory: af,
		pubkeyFactory:  pf,
	}
}

func (f rosterFactory) New(authority crypto.CollectiveAuthority) roster {
	addrs := make([]mino.Address, authority.Len())
	pubkeys := make([]crypto.PublicKey, authority.Len())

	addrIter := authority.AddressIterator()
	pubkeyIter := authority.PublicKeyIterator()
	for i := 0; addrIter.HasNext() && pubkeyIter.HasNext(); i++ {
		addrs[i] = addrIter.GetNext()
		pubkeys[i] = pubkeyIter.GetNext()
	}

	roster := roster{
		addrs:   addrs,
		pubkeys: pubkeys,
	}

	return roster
}

func (f rosterFactory) FromProto(in proto.Message) (viewchange.EvolvableAuthority, error) {
	var pb *Roster
	switch msg := in.(type) {
	case *Roster:
		pb = msg
	default:
		return nil, xerrors.Errorf("invalid message type '%T'", in)
	}

	if len(pb.Addresses) != len(pb.PublicKeys) {
		return nil, xerrors.Errorf("mismatch array length %d != %d",
			len(pb.Addresses), len(pb.PublicKeys))
	}

	addrs := make([]mino.Address, len(pb.Addresses))
	pubkeys := make([]crypto.PublicKey, len(pb.PublicKeys))

	for i, addrpb := range pb.GetAddresses() {
		addrs[i] = f.addressFactory.FromText(addrpb)

		pubkey, err := f.pubkeyFactory.FromProto(pb.GetPublicKeys()[i])
		if err != nil {
			return nil, xerrors.Errorf("couldn't decode public key: %v", err)
		}

		pubkeys[i] = pubkey
	}

	roster := roster{
		addrs:   addrs,
		pubkeys: pubkeys,
	}

	return roster, nil
}