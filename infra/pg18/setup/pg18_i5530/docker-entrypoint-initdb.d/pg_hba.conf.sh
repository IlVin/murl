#/bin/sh

echo "
# TYPE  DATABASE        USER            ADDRESS                 METHOD
host    replication     replicator      all                     scram-sha-256
host    all             murl            all                     password
" >> /var/lib/postgresql/18/docker/pg_hba.conf