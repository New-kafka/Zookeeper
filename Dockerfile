FROM ubuntu:20.04
RUN apt-get update && \
    apt-get install -y build-essential

WORKDIR /app
COPY ./bin/zookeeper /app/
COPY ./config/config.yml /app/config/
EXPOSE 8000

ENTRYPOINT ["/app/zookeeper"]