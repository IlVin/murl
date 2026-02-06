#/bin/bash

export PGPASSWORD=$POSTGRES_PASSWORD
pg_isready --host=pg18_i5530 --port=5530 --username=$POSTGRES_USER --dbname=$POSTGRES_USER || exit 1
