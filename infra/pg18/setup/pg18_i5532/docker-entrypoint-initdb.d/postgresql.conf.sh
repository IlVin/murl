#/bin/sh


sed -ri "s!^#?(wal_level)\s*=\s*\S+!\1 = replica!" /var/lib/postgresql/18/docker/postgresql.conf

sed -ri "s!^#?(max_wal_senders)\s*=\s*\S+!\1 = 10!" /var/lib/postgresql/18/docker/postgresql.conf

sed -ri "s!^#?(max_replication_slots)\s*=\s*\S+!\1 = 10!" /var/lib/postgresql/18/docker/postgresql.conf

sed -ri "s!^#?(synchronous_commit)\s*=\s*\S+!\1 = on!" /var/lib/postgresql/18/docker/postgresql.conf

sed -ri "s!^#?(shared_preload_libraries)\s*=\s*'([^']*)'!shared_preload_libraries = 'pg_cron'!" /var/lib/postgresql/18/docker/postgresql.conf
echo -e "\ncron.database_name = 'postgres'\n" >> /var/lib/postgresql/18/docker/postgresql.conf