FROM quay.io/redhat-services-prod/openshift/boilerplate:image-v6.0.1 AS builder

RUN mkdir /src

# Copy and download the dependecies so that they are cached locally in the stages.
COPY go.mod /src
COPY go.sum /src
WORKDIR /src
RUN go mod download

COPY . /src

RUN make gobuild

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.10-1130
ENV OPERATOR=/usr/local/bin/custom-domains-operator \
    USER_UID=1001 \
    USER_NAME=custom-domains-operator
ENV OPERATOR_BIN=custom-domains-operator

COPY --from=builder /src/build/_output/bin/${OPERATOR_BIN} /usr/local/bin/${OPERATOR_BIN}
COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
