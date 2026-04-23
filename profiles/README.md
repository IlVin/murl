File: main
Build ID: 3e60984ceacd161db02f6a2e17e2ddac9be21751
Type: inuse_space
Time: 2026-04-21 19:54:16 MSK
Duration: 360.04s, Total samples = 75.73MB 
Showing nodes accounting for -65.19MB, 86.08% of 75.73MB total
Dropped 8 nodes (cum <= 0.38MB)
      flat  flat%   sum%        cum   cum%
  -37.90MB 50.05% 50.05%   -49.11MB 64.85%  compress/flate.NewWriter (inline)
  -11.21MB 14.80% 64.85%   -11.21MB 14.80%  compress/flate.(*compressor).initDeflate (inline)
   -4.19MB  5.53% 70.38%    -4.19MB  5.53%  murl/internal/adapters/pgc.NewPgBatcher[go.shape.int,go.shape.struct { Idx uint64; Cf bool }]
   -3.22MB  4.25% 74.63%   -57.89MB 76.44%  murl/internal/handlers.(*Handlers).APIShortenBatch.func1
   -2.14MB  2.83% 77.46%    -2.14MB  2.83%  log.init.0.func1.1
   -1.50MB  1.98% 79.44%    -1.50MB  1.98%  compress/flate.(*huffmanEncoder).generate
      -1MB  1.32% 80.77%       -1MB  1.32%  bufio.NewWriterSize (inline)
      -1MB  1.32% 82.09%       -1MB  1.32%  murl/internal/adapters/pgc.(*pgBatcher[go.shape.struct { Idx uint64; Cf bool },go.shape.int]).worker
      -1MB  1.32% 83.41%       -1MB  1.32%  runtime.mallocgc
      -1MB  1.32% 84.73%       -1MB  1.32%  github.com/jackc/pgx/v5.(*Conn).getRows
   -0.52MB  0.68% 85.42%    -0.52MB  0.68%  murl/internal/adapters/pgc.NewPgBatcher[go.shape.int,go.shape.struct {}]
   -0.50MB  0.67% 86.08%    -0.50MB  0.67%  murl/internal/repository/pg.(*PgRepoLinksBySessionID).SelectAll-range1
    0.50MB  0.67% 85.42%     0.99MB  1.31%  murl/internal/service.(*Service).DeleteURLBySessionID
    0.50MB  0.66% 84.75%     0.50MB  0.66%  fmt.(*buffer).writeByte (inline)
   -0.50MB  0.66% 85.41%    -0.50MB  0.66%  bufio.NewReaderSize (inline)
   -0.50MB  0.66% 86.08%    -0.50MB  0.66%  sync.(*poolChain).pushHead
   -0.50MB  0.66% 86.74%    -0.50MB  0.66%  log/slog/internal/buffer.init.func1
    0.50MB  0.66% 86.08%     0.50MB  0.66%  internal/profile.(*Profile).postDecode
   -0.50MB  0.66% 86.74%    -0.50MB  0.66%  net/http.Header.Clone (inline)
    0.50MB  0.66% 86.08%     0.50MB  0.66%  internal/profile.init.func2
    0.50MB  0.66% 85.42%     0.50MB  0.66%  murl/internal/model/auditlog.(*Auditlog).Start.func1
   -0.50MB  0.66% 86.08%    -0.50MB  0.66%  encoding/base64.(*Encoding).DecodeString
         0     0% 86.08%    -0.50MB  0.66%  bufio.NewReader (inline)
         0     0% 86.08%    -0.53MB   0.7%  bytes.(*Buffer).Write
         0     0% 86.08%     0.57MB  0.75%  bytes.(*Buffer).WriteString
         0     0% 86.08%    -1.50MB  1.98%  compress/flate.(*Writer).Close (inline)
         0     0% 86.08%    -1.50MB  1.98%  compress/flate.(*compressor).close
         0     0% 86.08%    -1.50MB  1.98%  compress/flate.(*compressor).deflate
         0     0% 86.08%   -11.21MB 14.80%  compress/flate.(*compressor).init
         0     0% 86.08%    -1.50MB  1.98%  compress/flate.(*compressor).writeBlock
         0     0% 86.08%    -0.50MB  0.66%  compress/flate.(*huffmanBitWriter).indexTokens
         0     0% 86.08%    -1.50MB  1.98%  compress/flate.(*huffmanBitWriter).writeBlock
         0     0% 86.08%    -1.50MB  1.98%  compress/gzip.(*Writer).Close
         0     0% 86.08%   -49.11MB 64.85%  compress/gzip.(*Writer).Write
         0     0% 86.08%    -0.53MB   0.7%  encoding/json.stringEncoder
         0     0% 86.08%     0.50MB  0.66%  fmt.(*pp).doPrintf
         0     0% 86.08%     0.50MB  0.66%  fmt.(*pp).printArg
         0     0% 86.08%     0.50MB  0.66%  fmt.(*pp).printValue
         0     0% 86.08%     0.50MB  0.66%  fmt.Sprintf
         0     0% 86.08%        1MB  1.32%  github.com/go-chi/chi/v5.(*Mux).Mount.func1
         0     0% 86.08%   -62.15MB 82.06%  github.com/go-chi/chi/v5.(*Mux).ServeHTTP
         0     0% 86.08%   -60.14MB 79.41%  github.com/go-chi/chi/v5.(*Mux).routeHTTP
         0     0% 86.08%    -1.50MB  1.98%  github.com/jackc/pgx/v5.(*Conn).Query
         0     0% 86.08%    -0.50MB  0.66%  github.com/jackc/pgx/v5.(*Conn).QueryRow (inline)
         0     0% 86.08%     0.50MB  0.66%  github.com/jackc/pgx/v5.(*pipelineBatchResults).Query
         0     0% 86.08%       -1MB  1.32%  github.com/jackc/pgx/v5/pgxpool.(*Conn).Query
         0     0% 86.08%    -0.50MB  0.66%  github.com/jackc/pgx/v5/pgxpool.(*Conn).QueryRow
         0     0% 86.08%       -1MB  1.32%  github.com/jackc/pgx/v5/pgxpool.(*Pool).Query
         0     0% 86.08%    -0.50MB  0.66%  github.com/jackc/pgx/v5/pgxpool.(*Pool).QueryRow
         0     0% 86.08%     0.50MB  0.66%  github.com/jackc/pgx/v5/pgxpool.(*poolBatchResults).Query
         0     0% 86.08%    -1.50MB  1.99%  github.com/sony/gobreaker.(*CircuitBreaker).Execute
         0     0% 86.08%        1MB  1.32%  internal/profile.Parse
         0     0% 86.08%     0.50MB  0.66%  internal/profile.decodeMessage
         0     0% 86.08%        1MB  1.32%  internal/profile.parseUncompressed
         0     0% 86.08%     0.50MB  0.66%  internal/profile.unmarshal (inline)
         0     0% 86.08%    -2.14MB  2.83%  log.(*Logger).output
         0     0% 86.08%    -2.14MB  2.83%  log.init.0.func1
         0     0% 86.08%    -2.64MB  3.49%  log/slog.(*Logger).log
         0     0% 86.08%    -0.50MB  0.66%  log/slog.(*commonHandler).newHandleState
         0     0% 86.08%    -2.64MB  3.49%  log/slog.(*defaultHandler).Handle
         0     0% 86.08%     0.50MB  0.66%  log/slog.(*handleState).appendAttr
         0     0% 86.08%     0.50MB  0.66%  log/slog.(*handleState).appendNonBuiltIns
         0     0% 86.08%     0.50MB  0.66%  log/slog.(*handleState).appendNonBuiltIns.func1 (inline)
         0     0% 86.08%     0.50MB  0.66%  log/slog.(*handleState).appendValue
         0     0% 86.08%    -0.50MB  0.66%  log/slog.(*handleState).free
         0     0% 86.08%    -2.64MB  3.49%  log/slog.Info (partial-inline)
         0     0% 86.08%     0.50MB  0.66%  log/slog.Record.Attrs (inline)
         0     0% 86.08%     0.50MB  0.66%  log/slog.appendTextValue
         0     0% 86.08%    -0.50MB  0.66%  log/slog/internal/buffer.(*Buffer).Free (inline)
         0     0% 86.08%    -0.50MB  0.66%  log/slog/internal/buffer.New (inline)
         0     0% 86.08%     0.50MB  0.66%  murl/internal/adapters/pgc.(*pgBatcher[go.shape.struct {},go.shape.int]).execute
         0     0% 86.08%     0.50MB  0.66%  murl/internal/adapters/pgc.(*pgBatcher[go.shape.struct {},go.shape.int]).worker
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/adapters/pgc.(*pgConnector).Fetch.func1
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/adapters/pgc.(*pgConnector).Fetch.func1.1
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/adapters/pgc.(*pgConnector).FetchRow
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/adapters/pgc.(*pgConnector).FetchRow.func1
         0     0% 86.08%     0.50MB  0.66%  murl/internal/adapters/pgc.(*pgConnector).SendBatch.func1
         0     0% 86.08%     0.50MB  0.66%  murl/internal/adapters/pgc.(*pgConnector).SendBatch.func1.1
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/adapters/pgc.(*pgConnector).execInternal
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/adapters/pgc.(*pgConnector).execInternal.func1
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/adapters/pgc.FetchRow[go.shape.struct { URL string; Deleted bool }]
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/adapters/pgc.Fetch[go.shape.struct { Idx uint64; OriginalURL string }].func1
         0     0% 86.08%    -0.50MB  0.67%  murl/internal/adapters/pgc.Fetch[go.shape.struct { Idx uint64; OriginalURL string }].func1-range1
         0     0% 86.08%    -3.25MB  4.29%  murl/internal/handlers.(*Handlers).APIUserURLs.func1
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/handlers.(*Handlers).AddURL.func1
         0     0% 86.08%     0.99MB  1.31%  murl/internal/handlers.(*Handlers).DeleteAPIUserURLs.func1
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/handlers.(*Handlers).GetURL.func1
         0     0% 86.08%    -4.19MB  5.53%  murl/internal/handlers.(*Handlers).writePart
         0     0% 86.08%   -62.15MB 82.06%  murl/internal/handlers.newChiRouter.WithLimiter.func2.1
         0     0% 86.08%   -62.15MB 82.06%  murl/internal/handlers.newChiRouter.WithLogging.func3.1
         0     0% 86.08%        1MB  1.32%  murl/internal/handlers.newChiRouter.func1
         0     0% 86.08%    -1.50MB  1.98%  murl/internal/handlers/middleware.(*CompressResponseWriter).Close
         0     0% 86.08%   -49.11MB 64.85%  murl/internal/handlers/middleware.(*CompressResponseWriter).Write
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/handlers/middleware.(*CompressResponseWriter).WriteHeader
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/handlers/middleware.(*wrapResponseWriter).WriteHeader
         0     0% 86.08%   -61.65MB 81.40%  murl/internal/handlers/middleware.WithCompress.func1.6
         0     0% 86.08%   -62.15MB 82.06%  murl/internal/handlers/middleware.WithSession.func1.1
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/model.ParseShortPath
         0     0% 86.08%    -4.19MB  5.53%  murl/internal/repository/pg.(*PgRepoLinks).BatchUpSert
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/repository/pg.(*PgRepoLinks).Select
         0     0% 86.08%    -0.52MB  0.68%  murl/internal/repository/pg.(*PgRepoLinksBySessionID).BatchDelBySessionID
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/repository/pg.(*PgRepoLinksBySessionID).BatchDelBySessionID.func1
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/repository/pg.(*PgRepoLinksBySessionID).SelectAll
         0     0% 86.08%    -3.67MB  4.84%  murl/internal/repository/repo.(*repo).Batch
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/repository/repo.(*repo).GetURLBySessionID
         0     0% 86.08%    -2.54MB  3.36%  murl/internal/repository/repo.(*repo).On
         0     0% 86.08%    -0.52MB  0.69%  murl/internal/repository/repo.(*repo).evBatch
         0     0% 86.08%    -0.52MB  0.68%  murl/internal/repository/repo.(*repo).evDeleteURLBySessionID
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/repository/repo.(*repo).evGetURL
         0     0% 86.08%       -1MB  1.33%  murl/internal/repository/repo.(*repo).evGetURLBySessionID
         0     0% 86.08%    -4.19MB  5.53%  murl/internal/service.(*Service).Batch
         0     0% 86.08%    -0.50MB  0.66%  murl/internal/service.(*Service).GetURL
         0     0% 86.08%    -1.50MB  1.99%  murl/internal/service.(*Service).GetURLBySessionID
         0     0% 86.08%        1MB  1.32%  net/http.(*ServeMux).ServeHTTP
         0     0% 86.08%       -1MB  1.32%  net/http.(*conn).readRequest
         0     0% 86.08%   -63.65MB 84.05%  net/http.(*conn).serve
         0     0% 86.08%    -0.50MB  0.66%  net/http.(*response).WriteHeader
         0     0% 86.08%   -62.15MB 82.06%  net/http.HandlerFunc.ServeHTTP
         0     0% 86.08%    -0.50MB  0.66%  net/http.newBufioReader
         0     0% 86.08%       -1MB  1.32%  net/http.newBufioWriterSize
         0     0% 86.08%   -62.15MB 82.06%  net/http.serverHandler.ServeHTTP
         0     0% 86.08%        1MB  1.32%  net/http/pprof.Index
         0     0% 86.08%        1MB  1.32%  net/http/pprof.collectProfile
         0     0% 86.08%        1MB  1.32%  net/http/pprof.handler.ServeHTTP
         0     0% 86.08%        1MB  1.32%  net/http/pprof.handler.serveDeltaProfile
         0     0% 86.08%       -1MB  1.32%  runtime.allocm
         0     0% 86.08%       -1MB  1.32%  runtime.mstart
         0     0% 86.08%       -1MB  1.32%  runtime.mstart0
         0     0% 86.08%       -1MB  1.32%  runtime.mstart1
         0     0% 86.08%       -1MB  1.32%  runtime.newm
         0     0% 86.08%       -1MB  1.32%  runtime.newobject
         0     0% 86.08%       -1MB  1.32%  runtime.resetspinning
         0     0% 86.08%       -1MB  1.32%  runtime.schedule
         0     0% 86.08%       -1MB  1.32%  runtime.startm
         0     0% 86.08%       -1MB  1.32%  runtime.wakep
         0     0% 86.08%    -0.50MB  0.66%  sync.(*Pool).Get
         0     0% 86.08%    -0.50MB  0.66%  sync.(*Pool).Put
