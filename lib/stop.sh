stop() {
    docker stop "$CONTAINER_NAME" > /dev/null 2>&1
    echo "$CONTAINER_NAME has stopped"
}