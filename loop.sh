while true; do
  sleep 0.2 &
  echo "$(date -Iseconds)"
  wait
done
