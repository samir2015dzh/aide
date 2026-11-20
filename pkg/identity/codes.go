package identity

import (
	"crypto/rand"
	"encoding/base32"
)

// Crockford base32 avoids I, L, O, U.
var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// RandomCode returns n random Crockford base32 characters.
func RandomCode(n int) string {
	b := make([]byte, (n*5+7)/8)
	rand.Read(b)
	return crockford.EncodeToString(b)[:n]
}
