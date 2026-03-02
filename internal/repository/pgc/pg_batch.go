package pgc

type BatchExec interface {
	Add(SQL string, prms []any)
	Flush()
}
