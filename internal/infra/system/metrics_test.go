package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	assert.NotNil(t, m)
}

func TestMetrics_CPUPercent(t *testing.T) {
	m := NewMetrics()
	pct, err := m.CPUPercent()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, pct, 0.0)
}

func TestMetrics_MemAvailMB(t *testing.T) {
	m := NewMetrics()
	mb, err := m.MemAvailMB()
	require.NoError(t, err)
	assert.Greater(t, mb, uint64(0))
}
