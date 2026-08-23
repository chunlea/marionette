package storemock

// storemock holds generated gomock implementations of store.Store and store.Tx.
//
// Use it for tests that only need to satisfy the interface or assert that a
// particular call was made. Tests that need the store to actually behave like a
// store — read back what was written, enforce uniqueness — want either the
// testcontainers harness in pkg/store/postgres or a behavioural fake, not a
// call recorder.
//
// Regenerate with `make generate` after changing store.Store or store.Tx. The
// package doc itself lives on the generated file.

import "github.com/chunlea/marionette/pkg/store"

// Adding a method to store.Store or store.Tx without regenerating this package
// must fail the build here, rather than at the first test that uses the mock.
var (
	_ store.Store = (*MockStore)(nil)
	_ store.Tx    = (*MockTx)(nil)
)
