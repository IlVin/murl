#/bin/bash

export PGPASSWORD=$POSTGRES_PASSWORD
pg_isready --host=pg18_i5533 --port=5533 --username=$POSTGRES_USER --dbname=$POSTGRES_USER || exit 1
psql --host=pg18_i5533 --port=5533 --username=$POSTGRES_USER --dbname=$POSTGRES_USER -tA -c "SELECT pg_is_in_recovery()" | grep -q t || exit 1