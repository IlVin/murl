package internal

//go:generate $GOPATH/bin/mockgen                                                      -destination=mocks/pgx_mock.go            -package=mocks github.com/jackc/pgx/v5 Tx,Row,BatchResults
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/backoff/backoff.go            -destination=mocks/pgc_backoff_mock.go    -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/metrics/prometheus_metrics.go -destination=mocks/pgc_metrics_mock.go    -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/instance/pg_instance.go       -destination=mocks/pgc_pginstance_mock.go -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/pgc/pg_cluster.go                 -destination=mocks/pgc_pgcluster_mock.go  -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/wal/wal.go                        -destination=mocks/wal_mock.go            -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/repo_links.go                     -destination=mocks/repo_links_mock.go     -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/inmem/core.go                     -destination=mocks/inmem_core_mock.go     -package=mocks
//go:generate $GOPATH/bin/mockgen -source=repository/repo.go                           -destination=mocks/repo_mock.go           -package=mocks
//go:generate $GOPATH/bin/mockgen -source=model/event/event.go                         -destination=mocks/event_mock.go          -package=mocks
//go:generate $GOPATH/bin/mockgen -source=service/service.go                           -destination=mocks/service_mock.go        -package=mocks
//go:generate $GOPATH/bin/mockgen -source=handlers/handlers.go                         -destination=mocks/handlers_mock.go       -package=mocks
