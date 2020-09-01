FROM registry.svc.ci.openshift.org/openshift/release:golang-1.14 AS builder

RUN mkdir /custom-domains-operator

# Copy and download the dependecies so that they are cached locally in the stages.
COPY go.mod /custom-domains-operator
COPY go.sum /custom-domains-operator
WORKDIR /custom-domains-operator
RUN go mod download

COPY . /custom-domains-operator

RUN make gobuild

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV OPERATOR=/usr/local/bin/custom-domains-operator \
    USER_UID=1001 \
    USER_NAME=custom-domains-operator
ENV OPERATOR_BIN=custom-domains-operator

COPY --from=builder /custom-domains-operator/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
