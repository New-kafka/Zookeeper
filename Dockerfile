FROM ubuntu:20.04
RUN apt-get update && \
    apt-get install -y build-essential

WORKDIR /app
COPY ./bin/zookeeper ./
COPY config/sample-config.yml ./config/config.yml
EXPOSE 8000
ENTRYPOINT ["/app/zookeeper"]

WORKDIR /app

COPY patroni.yml /app/patroni.yml

EXPOSE 8008 5432

ENTRYPOINT ["patroni", "/app/patroni.yml"]

