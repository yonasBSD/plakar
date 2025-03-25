package snapshot_test

import (
	"testing"

	"github.com/PlakarKorp/plakar/encryption/keypair"
	"github.com/stretchr/testify/require"
)

// will be fixed when signed snapshots are back post beta
func _TestVerify(t *testing.T) {
	defaultKeyPair, err := keypair.Generate()
	require.NoError(t, err)

	snap := generateSnapshot(t, defaultKeyPair)
	defer snap.Close()

	verified, err := snap.Verify()
	require.NoError(t, err)
	require.True(t, verified)
}
