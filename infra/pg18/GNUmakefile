.DEFAULT_GOAL := info

# Инстансы PostgreSQL
PINST=5530 5532
SINST=5531 5533
INST?=${PINST} ${SINST}

PG_PREFIX=pg18
BACKUP_TAG := $(shell test -n "\${BACKUP_TAG}" && echo \${BACKUP_TAG}; test ! -n "\${BACKUP_TAG}" && date +backup-%Y.%m.%d_%H%M%S )
NETWORK="devnet"

POSTGRES_USER=root
POSTGRES_PASSWORD=postal

# ### INFO ###
info:
	@docker network ls -f NAME=${NETWORK} &&	echo "";
	@docker volume ls -f NAME=${PG_PREFIX} && echo "";
	@docker ps -a -f NAME=${PG_PREFIX} && echo "";


# ### SETUP ###
setup: down images network volumes
	@echo "===> Init DB"
	@echo -n "${INST}" | xargs -r --delimiter=" " -I {} \
		docker run --detach --network=${NETWORK} -p {}:{} \
			--name ${PG_PREFIX}_i{} \
			--hostname ${PG_PREFIX}_i{} \
			--domainname ${PG_PREFIX}_i{} \
			-v ${PG_PREFIX}_i{}:/var/lib/postgresql/ \
			-v ./setup/${PG_PREFIX}_i{}/docker-entrypoint-initdb.d:/docker-entrypoint-initdb.d \
			-v ./setup/${PG_PREFIX}_i{}/HEALTHCHECK.sh:/HEALTHCHECK.sh \
			-e POSTGRES_USER=${POSTGRES_USER} \
			-e POSTGRES_PASSWORD=${POSTGRES_PASSWORD} \
			${PG_PREFIX}_i{};

	@$(MAKE) wait_db INST="${INST}"
	@$(MAKE) down INST="${SINST}"
	@$(MAKE) volumes INST="${SINST}"

	@echo "===> Init Replicas"
	@( for ID in ${SINST}; do \
		docker run -i --rm -v ${PG_PREFIX}_i$${ID}:/var/lib/postgresql/ ${PG_PREFIX}_i$${ID} sh -c 'mkdir -p /var/lib/postgresql/18/docker && chown postgres:postgres /var/lib/postgresql/18/docker'; \
		docker run -i --network=${NETWORK} \
			--name ${PG_PREFIX}_i$${ID} \
			-v ${PG_PREFIX}_i$${ID}:/var/lib/postgresql/ \
			-v ./setup/${PG_PREFIX}_i$${ID}/Standby.sh:/Standby.sh \
			-e POSTGRES_USER=${POSTGRES_USER} \
			-e POSTGRES_PASSWORD=${POSTGRES_PASSWORD} \
			${PG_PREFIX}_i$${ID} \
			/Standby.sh; \
	done );

	@$(MAKE) logs INST="${INST}"
	@$(MAKE) down INST="${INST}"

images:
	@echo "===> Build pg18_i* images"
	@echo -n "${INST}" | xargs -r --delimiter=" " -I {} \
		docker build \
			-t ${PG_PREFIX}_i{} \
			-f ./setup/${PG_PREFIX}_i{}/Dockerfile \
			./setup/${PG_PREFIX}_i{}
	@echo "===> Build pg18_data image"
	@docker build \
		-t ${PG_PREFIX}_data \
		-f ./${PG_PREFIX}_data.Dockerfile \
		.

network:
	@echo "===> Remove NETWORK ${NATWORK}"
	@docker network ls -f NAME=${NETWORK} -q | xargs -r docker network rm
	@echo "===> Create NETWORK ${NATWORK}"
	@docker network create ${NETWORK}

volumes:
	@echo "===> Remove volumes"
	@echo -n "${INST}" | xargs -r --delimiter=' ' -I {} \
		docker volume rm ${PG_PREFIX}_i{}
	@echo "===> Create volumes"
	@echo -n "${INST}" | xargs -r --delimiter=' ' -I {} \
		docker volume create ${PG_PREFIX}_i{}

logs:
	@( for ID in ${INST}; do \
		DID=`docker ps -a -f NAME=${PG_PREFIX}_i$${ID} -q`; \
		if [ "" != "$${DID}" ]; then \
			echo "\n\nvvvvvvvvvvvvvv LOGS for ${PG_PREFIX}_i$${ID} ($${DID}) vvvvvvvvvvvvvv"; \
			docker logs $${DID}; \
			echo "\n^^^^^^^^^^^^^^ LOGS for ${PG_PREFIX}_i$${ID} ($${DID}) ^^^^^^^^^^^^^^\n\n"; \
		fi; \
	done );

wait_db:
	@echo "===> WAIT DB"
	@( for ID in ${INST}; do \
		while ! docker run --rm --network=$(NETWORK) $(PG_PREFIX)_i$$ID pg_isready --host=$(PG_PREFIX)_i$$ID --port $$ID --username=${POSTGRES_USER} --dbname=${POSTGRES_USER}; do \
			echo "Wait ${PG_PREFIX}_i$${ID} 1s"; \
			sleep 1; \
		done; \
	done );

# ### UP ###
up: down
	@echo "===> UP"
	@echo -n "${INST}" | xargs -r --delimiter=" " -I {} \
		docker run --detach --network=${NETWORK} -p {}:{} \
			--name ${PG_PREFIX}_i{} \
			--hostname ${PG_PREFIX}_i{} \
			--domainname ${PG_PREFIX}_i{} \
			-v ${PG_PREFIX}_i{}:/var/lib/postgresql/ \
			-v ./setup/${PG_PREFIX}_i{}/HEALTHCHECK.sh:/HEALTHCHECK.sh \
			-e POSTGRES_USER=${POSTGRES_USER} \
			-e POSTGRES_PASSWORD=${POSTGRES_PASSWORD} \
			${PG_PREFIX}_i{}


# ### DOWN ###
down: stop
	@echo "===> WAIT"
	@( echo -n "${INST}" | xargs -r --delimiter=" " -I {} docker ps -a -q -f NAME=${PG_PREFIX}_i{} ) | xargs -r -I {{}} \
		docker wait {{}} > /dev/null;
	@echo "===> RM"
	@( echo -n "${INST}" | xargs -r --delimiter=" " -I {} docker ps -a -q -f NAME=${PG_PREFIX}_i{} ) | xargs -r -I {{}} \
		docker rm {{}} > /dev/null;

stop:
	@echo "===> STOP"
	@( echo -n "${INST}" | xargs -r --delimiter=" " -I {} docker ps -a -q -f NAME=${PG_PREFIX}_i{} ) | xargs -r -I {{}} \
		docker stop --time=60 {{}} > /dev/null;



# ### CHECKOUT ###
checkout: down volumes
	@echo "===> CHECKOUT"
	@( for ID in ${INST}; do \
		echo -n $${ID} | xargs -r --delimiter=' ' -I {} \
			docker run --detach \
				--name ${PG_PREFIX}_data_i{} \
				-v ${PG_PREFIX}_i{}:/volume_${PG_PREFIX}_i{} \
				${PG_PREFIX}_data_i{}:latest \
				sh -c '/bin/cp -a -r /${PG_PREFIX}_i{}/* /volume_${PG_PREFIX}_i{}/  && chown -r postgres:postgres /volume_${PG_PREFIX}_i{}/*'; \
		docker wait ${PG_PREFIX}_data_i$${ID}; \
		docker rm ${PG_PREFIX}_data_i$${ID}; \
	done );

# ### COMMIT ###
commit: down
	@echo "===> COMMIT"
	@( for ID in ${INST}; do \
		echo -n $${ID} | xargs -r --delimiter=' ' -I {} \
			docker run --detach \
				--name ${PG_PREFIX}_data_i{} \
				-v ${PG_PREFIX}_i{}:/volume_${PG_PREFIX}_i{} \
				${PG_PREFIX}_data:latest \
				sh -c '/bin/mkdir -p /${PG_PREFIX}_i{} && /bin/cp -a -r /volume_${PG_PREFIX}_i{}/* /${PG_PREFIX}_i{}/'; \
		docker wait ${PG_PREFIX}_data_i$${ID}; \
		docker tag ${PG_PREFIX}_data_i$${ID}:latest ${PG_PREFIX}_data_i$${ID}:${BACKUP_TAG}; \
		docker commit ${PG_PREFIX}_data_i$${ID} ${PG_PREFIX}_data_i$${ID}:latest; \
		docker rm ${PG_PREFIX}_data_i$${ID}; \
	done );

# ### PSQL SHELL ###
pg18_i5530:
	export PGPASSWORD='postal' && psql -h localhost -p 5530 -Uroot -w

pg18_i5531:
	export PGPASSWORD='postal' && psql -h localhost -p 5531 -Uroot -w

pg18_i5532:
	export PGPASSWORD='postal' && psql -h localhost -p 5532 -Uroot -w

pg18_i5533:
	export PGPASSWORD='postal' && psql -h localhost -p 5533 -Uroot -w

