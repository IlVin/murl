package internal

//go:generate $GOPATH/bin/mockgen                                                      -destination=mocks/pgx_mocks_test.go            -package=mocks github.com/jackc/pgx/v5 Tx,Row,BatchResults
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/backoff/backoff.go            -destination=mocks/pgc_backoff_mocks_test.go    -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/metrics/prometheus_metrics.go -destination=mocks/pgc_metrics_mocks_test.go    -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/instance/pg_instance.go       -destination=mocks/pgc_pginstance_mocks_test.go -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/pg_cluster.go                 -destination=mocks/pgc_pgcluster_mocks_test.go  -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/wal/wal.go                        -destination=mocks/wal_mocks_test.go            -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/repo_links.go                     -destination=mocks/repo_links_mocks_test.go     -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/inmem/core.go                     -destination=mocks/inmem_core_mocks_test.go     -package=mocks
//go:generate $GOPATH/bin/mockgen -source=model/event/event.go                         -destination=mocks/event_mocks_test.go          -package=mocks
