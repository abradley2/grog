while true; do
  sleep 0.016 &
  echo "$(date +"%Y-%m-%d %H:%M:%S,%3N")"
  wait
done
