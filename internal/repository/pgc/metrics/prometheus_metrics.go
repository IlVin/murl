package metrics

import "github.com/prometheus/client_golang/prometheus"

type PrometheusPgMetrics struct {
	status  *prometheus.GaugeVec
	offline *prometheus.CounterVec
	retries *prometheus.CounterVec
	latency *prometheus.HistogramVec
}

func NewPgMetrics(reg prometheus.Registerer) (*PrometheusPgMetrics, error) {
	// Кастомные бакеты для БД: от 1мс до 5сек.
	dbBuckets := []float64{.001, .002, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

	m := &PrometheusPgMetrics{
		status: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pg_instance_ready",
			Help: "1 if instance is online, 0 if offline",
		}, []string{"instance"}),

		offline: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pg_instance_offline_total",
			Help: "Total number of offline events",
		}, []string{"instance"}),

		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pg_instance_retries_total",
			Help: "Total number of retries",
		}, []string{"instance", "type"}),

		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pg_instance_duration_seconds",
			Help:    "Duration of operations",
			Buckets: dbBuckets, // Используем наши бакеты
		}, []string{"instance", "type"}),
	}

	collectors := []prometheus.Collector{m.status, m.offline, m.retries, m.latency}
	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			// Если метрика уже зарегистрирована, Register вернет AlreadyRegisteredError
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return nil, err
			}
		}
	}

	return m, nil
}

func (m *PrometheusPgMetrics) SetStatus(inst string, online bool) {
	val := 0.0
	if online {
		val = 1.0
	}
	m.status.WithLabelValues(inst).Set(val)
}

func (m *PrometheusPgMetrics) IncOfflineEvent(inst string) {
	m.offline.WithLabelValues(inst).Inc()
}

func (m *PrometheusPgMetrics) IncRetry(inst string, opType string) {
	m.retries.WithLabelValues(inst, opType).Inc()
}

func (m *PrometheusPgMetrics) ObserveLatency(inst string, opType string, d float64) {
	m.latency.WithLabelValues(inst, opType).Observe(d)
}
