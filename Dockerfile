FROM ubuntu:20.04
RUN apt-get update && \
    apt-get install -y build-essential

WORKDIR /app
COPY ./bin/zookeeper ./
COPY ./config/config.yml ./config/config.yml
EXPOSE 8000

ENTRYPOINT ["/app/zookeeper"]
