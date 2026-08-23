//go:build !unix

package cas

import "errors"

// makeFIFO is only meaningful where named pipes exist.
func makeFIFO(string) error {
	return errors.New("named pipes are not available on this platform")
}
