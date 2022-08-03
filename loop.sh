while true; do
  sleep 1 &
  echo "$(date -Iseconds)"
  wait
done
