package pgc

// var reDangerous = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|MERGE)\b`)
// var reSelectStar = regexp.MustCompile(`(?is)SELECT\s+(\*|([^f]|f[^r]|fr[^o]|fro[^m])*?([a-z_]+\.\*|,\s*\*))`)

//	normalized, err := pg_query.Normalize(sql)
//	if err != nil {
//		panic(fmt.Errorf("pgc [%s]: sql normalization error: %w\nSQL: %s", name, err, sql))
//	}
//
//	if reSelectStar.MatchString(normalized) {
//		panic(fmt.Sprintf("pgc [%s]: '*' is forbidden. List all columns explicitly.\nSQL: %s", name, sql))
//	}
//
//	readOnly := !reDangerous.MatchString(normalized)
//
//	result, err := pg_query.Parse(sql)
//	if err != nil {
//		panic(fmt.Errorf("pgc [%s]: syntax error: %w\nSQL: %s", name, err, sql))
//	}
//
//	if len(result.Stmts) > 1 {
//		panic(fmt.Sprintf("pgc [%s]: multi-statements (;) are forbidden. Use CTE.", name))
//	}
//
//	rootNode := result.Stmts[0].GetStmt()
//
//	var sqlCols int
//	var hasReturns bool
//
//	if sel := rootNode.GetSelectStmt(); sel != nil {
//		sqlCols = len(sel.TargetList)
//		hasReturns = true
//	} else if ins := rootNode.GetInsertStmt(); ins != nil {
//		sqlCols = len(ins.GetReturningList())
//		hasReturns = sqlCols > 0
//	} else if upd := rootNode.GetUpdateStmt(); upd != nil {
//		sqlCols = len(upd.GetReturningList())
//		hasReturns = sqlCols > 0
//	} else if del := rootNode.GetDeleteStmt(); del != nil {
//		sqlCols = len(del.GetReturningList())
//		hasReturns = sqlCols > 0
//	}
//
//	if hasReturns {
//		if binder == nil {
//			panic(fmt.Sprintf("pgc [%s]: query returns %d columns, but binder is nil", name, sqlCols))
//		}
//		if bCount := len(binder(&dummy)); bCount != sqlCols {
//			panic(fmt.Sprintf("pgc [%s]: binder fields (%d) != SQL columns (%d)", name, bCount, sqlCols))
//		}
//	} else if binder != nil {
//		panic(fmt.Sprintf("pgc [%s]: binder provided for query with no results", name))
//	}
//

//  // Query выполняет запрос и возвращает итератор Go 1.23 для ленивого чтения строк.
//  func (aq *ActiveQuery[T]) Query(ctx context.Context, args ...any) iter.Seq2[T, error] {
//  	ctx, span := aq.pg.OTel().Tracer().Start(ctx, aq.query.Name,
//  		trace.WithAttributes(
//  			attribute.String("db.system", "postgresql"),
//  			attribute.String("db.operation.name", "query"), // Стандарт для SELECT
//  			attribute.String("db.query.text", aq.query.SQL),
//  		))
//
//  	return func(yield func(T, error) bool) {
//  		defer span.End() // Спан закроется, когда итерация завершится
//
//  		var zero T
//  		_, err := aq.pg.PgPool(ctx, func(pCtx context.Context, pool PgxPoolIface) (any, error) {
//  			rows, err := pool.Query(pCtx, aq.query.SQL, args...)
//  			if err != nil {
//  				return nil, err
//  			}
//  			defer rows.Close()
//
//  			for rows.Next() {
//  				var item T
//  				if err := rows.Scan(aq.query.Binder(&item)...); err != nil {
//  					return nil, err
//  				}
//  				if !yield(item, nil) {
//  					return nil, nil
//  				}
//  			}
//  			return nil, rows.Err()
//  		})
//  		if err != nil {
//  			span.RecordError(err)
//  			span.SetStatus(codes.Error, err.Error())
//  			yield(zero, err)
//  		}
//  	}
//  }
//
//  // QueryRow выполняет запрос и возвращает ровно одну строку.
//  // ОШИБКА, если вызван для запроса, который не возвращает данных (используйте Exec).
//  func (aq *ActiveQuery[T]) QueryRow(ctx context.Context, args ...any) (T, error) {
//  	ctx, span := aq.pg.OTel().Tracer().Start(ctx, aq.query.Name,
//  		trace.WithAttributes(
//  			attribute.String("db.system", "postgresql"),
//  			attribute.String("db.operation.name", "query_row"),
//  			attribute.String("db.query.text", aq.query.SQL),
//  		))
//  	defer span.End()
//
//  	var item T
//  	if !aq.query.HasReturns {
//  		err := fmt.Errorf("pgc [%s]: QueryRow called on query with no results", aq.query.Name)
//  		span.RecordError(err)
//  		return item, err
//  	}
//
//  	_, err := aq.pg.PgPool(ctx, func(pCtx context.Context, pool PgxPoolIface) (any, error) {
//  		return nil, pool.QueryRow(pCtx, aq.query.SQL, args...).Scan(aq.query.Binder(&item)...)
//  	})
//  	if err != nil {
//  		span.RecordError(err)
//  	}
//
//  	return item, err
//  }
//
//  // Exec исполняет команду (INSERT/UPDATE/DELETE) и возвращает количество затронутых строк.
//  func (aq *ActiveQuery[T]) Exec(ctx context.Context, args ...any) (int64, error) {
//  	ctx, span := aq.pg.OTel().Tracer().Start(ctx, aq.query.Name,
//  		trace.WithAttributes(
//  			attribute.String("db.system", "postgresql"),
//  			attribute.String("db.operation.name", "exec"),
//  			attribute.String("db.query.text", aq.query.SQL),
//  		))
//  	defer span.End()
//
//  	var rowsAffected int64
//  	_, err := aq.pg.PgPool(ctx, func(pCtx context.Context, pool PgxPoolIface) (any, error) {
//  		tag, err := pool.Exec(pCtx, aq.query.SQL, args...)
//  		if err != nil {
//  			return nil, err
//  		}
//  		rowsAffected = tag.RowsAffected()
//  		return nil, nil
//  	})
//  	if err != nil {
//  		span.RecordError(err)
//  	}
//  	if err == nil {
//  		span.SetAttributes(attribute.Int64("db.response.rows_affected", rowsAffected))
//  	}
//  	return rowsAffected, err
//  }
//
//  // --- HELPERS ---
//
//  // Fetch извлекает поток данных в стиле Go 1.23 Iterators.
//  func Fetch[T any](ctx context.Context, pg PgInstance, q *Query[T], args ...any) iter.Seq2[T, error] {
//  	rawSeq := pg.Fetch(ctx, q, args...)
//  	return func(yield func(T, error) bool) {
//  		for val, err := range rawSeq {
//  			if err != nil {
//  				var zero T
//  				if !yield(zero, err) {
//  					return
//  				}
//  				continue
//  			}
//  			// val — это *T, возвращенный из драйвера
//  			if !yield(*(val.(*T)), nil) {
//  				return
//  			}
//  		}
//  	}
//  }
//
//  // FetchRow извлекает ровно одну строку.
//  func FetchRow[T any](ctx context.Context, pg PgInstance, q *Query[T], args ...any) (T, error) {
//  	var zero T
//  	res, err := pg.FetchRow(ctx, q, args...)
//  	if err != nil {
//  		return zero, err
//  	}
//  	return *(res.(*T)), nil
//  }
//
//  // Exec выполняет команду (INSERT/UPDATE/DELETE).
//  func Exec[T any](ctx context.Context, pg PgInstance, q *Query[T], args ...any) (int64, error) {
//  	return pg.Exec(ctx, q, args...)
//  }
//
