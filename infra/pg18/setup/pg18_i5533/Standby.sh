#/bin/bash

rm -rf /var/lib/postgresql/18/docker/*

export PGPASSWORD=${POSTGRES_PASSWORD}
until pg_isready -h pg18_i5532 -p 5532 -U ${POSTGRES_USER}; do
    echo "Wait 1 sec..."
    sleep 1
done

export PGPASSWORD=replicator_pswd
pg_basebackup -h pg18_i5532 -p 5532 -D /var/lib/postgresql/18/docker -U replicator --checkpoint=fast -v -X stream -P -R