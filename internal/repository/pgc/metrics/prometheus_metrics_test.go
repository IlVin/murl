package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPgMetrics(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		m, err := NewPgMetrics(reg)
		require.NoError(t, err)
		assert.NotNil(t, m)
	})

	t.Run("idempotent registration", func(t *testing.T) {
		reg := prometheus.NewRegistry()

		// Первый раз
		_, err := NewPgMetrics(reg)
		require.NoError(t, err)

		// Второй раз с тем же реестром не должен паниковать или возвращать ошибку
		m, err := NewPgMetrics(reg)
		assert.NoError(t, err, "Should handle AlreadyRegisteredError internally")
		assert.NotNil(t, m)
	})
}

func TestPrometheusPgMetrics_Values(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := NewPgMetrics(reg)
	require.NoError(t, err)

	inst := "localhost:5432"

	t.Run("status gauge", func(t *testing.T) {
		m.SetStatus(inst, true)
		assert.Equal(t, float64(1), testutil.ToFloat64(m.status.WithLabelValues(inst)))

		m.SetStatus(inst, false)
		assert.Equal(t, float64(0), testutil.ToFloat64(m.status.WithLabelValues(inst)))
	})

	t.Run("offline counter", func(t *testing.T) {
		m.IncOfflineEvent(inst)
		m.IncOfflineEvent(inst)
		assert.Equal(t, float64(2), testutil.ToFloat64(m.offline.WithLabelValues(inst)))
	})

	t.Run("retry counter", func(t *testing.T) {
		m.IncRetry(inst, "tx")
		m.IncRetry(inst, "tx")
		assert.Equal(t, float64(2), testutil.ToFloat64(m.retries.WithLabelValues(inst, "tx")))
	})

	t.Run("latency histogram", func(t *testing.T) {
		m.ObserveLatency(inst, "query", 0.005) // 5ms
		m.ObserveLatency(inst, "query", 0.010) // 10ms

		// Проверяем количество сэмплов в гистограмме (Sample Count)
		// testutil.CollectAndCount возвращает общее число метрик в векторе
		count := testutil.CollectAndCount(m.latency)
		assert.Equal(t, 1, count, "Should have one label set in histogram vector")
	})
}
