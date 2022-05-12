logs() {

    EXTRA_ARGS=""
    while :; do
        case $1 in
            logs)
                ;;
            -f)
                EXTRA_ARGS="$EXTRA_ARGS -f"
                ;;
            -t)
                if [ -n "$2" ]; then
                    EXTRA_ARGS="$EXTRA_ARGS -n $2"
                    shift
                else
                    echo "Error: \"-t\" requires a non empty argument"
                    exit 1
                fi
                ;;
            --)              # End of all options.
                shift
                break
                ;;
            -?*)
                printf 'WARN: Unknown option (ignored): %s\n' "$1" >&2
                ;;
            *)               # Default case: If no more options then break out of the loop.
                break
        esac
        shift
    done

    if [ ! "$(docker ps -q -f name="$CONTAINER_NAME")" ]; then
        echo "$CONTAINER_NAME is not running. No logs to display"
        exit 1
    fi

    docker logs $EXTRA_ARGS $CONTAINER_NAME
}