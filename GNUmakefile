
WORKDIR=/home/ilvin/GitHub/IlVin/murl


build:
	cd ${WORKDIR}/cmd/shortener && go build -o shortener *.go

iter1:
	cd ${WORKDIR}/cmd/shortener && ${WORKDIR}/cmd/tests/shortenertest -test.v -test.run=^TestIteration1$$ -binary-path=${WORKDIR}/cmd/shortener/shortener

iter2:
	cd ${WORKDIR}/cmd/shortener && ${WORKDIR}/cmd/tests/shortenertest -test.v -test.run=^TestIteration2$$ -binary-path=${WORKDIR}/cmd/shortener/shortener -source-path=${WORKDIR}/internal
