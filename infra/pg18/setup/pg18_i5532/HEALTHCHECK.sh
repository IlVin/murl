#/bin/bash

export PGPASSWORD=$POSTGRES_PASSWORD
pg_isready --host=pg18_i5532 --port=5532 --username=$POSTGRES_USER --dbname=$POSTGRES_USER || exit 1