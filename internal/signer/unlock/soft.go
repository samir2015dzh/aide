package unlock

import (
	"crypto/ecdsa"
	"errors"
	"sync"

	"github.com/hermeshai/hermesh/pkg/identity"
)

// SoftVault keeps software keys in memory, each opened by its PIN as the
// TPM's are: for tests, on machines without a TPM.
type SoftVault struct {
	mu   sync.Mutex
	keys map[string]softKey
}

type softKey struct {
	key *identity.SoftwareKey
	pin string
}

func (v *SoftVault) Create(pin string) (string, *ecdsa.PublicKey, error) {
	k, err := identity.GenerateKey()
	if err != nil {
		return "", nil, err
	}
	name := "Hermesh " + identity.RandomCode(16)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.keys == nil {
		v.keys = map[string]softKey{}
	}
	v.keys[name] = softKey{k, pin}
	return name, k.Public(), nil
}

func (v *SoftVault) Public(name string) (*ecdsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	k, ok := v.keys[name]
	if !ok {
		return nil, errors.New("no such key")
	}
	return k.key.Public(), nil
}

func (v *SoftVault) Sign(name, pin string, messages [][]byte) ([][]byte, error) {
	v.mu.Lock()
	k, ok := v.keys[name]
	v.mu.Unlock()
	if !ok || k.pin != pin {
		return nil, errors.New("the key's PIN is wrong")
	}
	sigs := make([][]byte, len(messages))
	for i, m := range messages {
		var err error
		if sigs[i], err = k.key.SignMessage(m); err != nil {
			return nil, err
		}
	}
	return sigs, nil
}

func (v *SoftVault) Delete(name, _ string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.keys, name)
	return nil
}
