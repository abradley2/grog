while true; do
  sleep 0.01 &
  echo "$(date +"%Y-%m-%d %H:%M:%S,%3N")"
  wait
done
