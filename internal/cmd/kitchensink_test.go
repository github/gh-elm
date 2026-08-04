package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteKitchenSink(t *testing.T) {
	var out bytes.Buffer

	require.NoError(t, writeKitchenSink(&out))

	rendered := out.String()
	assert.Contains(t, rendered, "TYPOGRAPHY")
	assert.Contains(t, rendered, "SEMANTIC COLORS")
	assert.Contains(t, rendered, "OUTPUT EXAMPLES")
	assert.Contains(t, rendered, "INTERACTIVE CONTROLS")
	assert.Contains(t, rendered, "⚠ Warning")
	assert.Contains(t, rendered, "✗ Error")
	assert.Contains(t, rendered, "Submit the empty first input")
}

func TestKitchenSinkCommand(t *testing.T) {
	root := NewRootCmd("test")

	command, _, err := root.Find([]string{"kitchensink"})
	require.NoError(t, err)
	assert.Equal(t, "kitchensink", command.Name())
}
