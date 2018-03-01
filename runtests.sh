#!/bin/bash

PORT=3306

wait_for_mysql() {
    echo "Waiting for MySQL server to become ready"
    cmd="mysql -h 127.1 -P 3306 -u root -ss -e 'SHOW STATUS' &> /dev/null"
    eval $cmd
    local status=$?
    
    while [ "$status" != "0" ]; do
       sleep 5
       eval $cmd
       status=$?
    done

}

for version in 5.6 5.7 8.0.3; do
    CONTAINER_NAME="test-mysql-${version}"
    echo "Starting container ${CONTAINER_NAME}"
    docker run --name $CONTAINER_NAME -e MYSQL_ALLOW_EMPTY_PASSWORD=true -d -p ${PORT}:3306 mysql:${version}
    wait_for_mysql

    go test -v

    echo "Shutting down ${CONTAINER_NAME}"
    docker stop $CONTAINER_NAME
    docker rm $CONTAINER_NAME
done

