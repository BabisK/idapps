clean() {
    if [ "$(docker ps -q -f name="$CONTAINER_NAME")" ]; then
        echo "Cannot clean when container $CONTAINER_NAME is running. Please stop the container first."
        exit 1
    fi

    docker volume rm "$VOLUME_NAME" > /dev/null 2>&1
    echo "Cleaned up data for $CONTAINER_NAME"
}