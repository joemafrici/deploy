#!/bin/bash

URL="http://gojoe.dev/hello_world"
REQUESTS_PER_SEC=1
DURATION_SECS=180

end_time=$((SECONDS + DURATION_SECS))

while [ $SECONDS -lt $end_time ]; do
    curl -s -o /dev/null -w "%{http_code}\n" $URL &
    sleep 0.33
done
