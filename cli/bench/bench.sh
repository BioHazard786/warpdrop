#!/bin/bash
# usage: bench.sh <old|new> <big|small> <runs>
S=${BENCH_DIR:-$(pwd)}
cd $S
tool=$1; set=$2; runs=$3
strip() { sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tr '\r' '\n'; }
if [ $set = big ]; then files=(bench/big/one.bin); want_n=1; elif [ $set = mid ]; then files=($(for i in $(seq 1 200); do echo bench/small/f$i.bin; done)); want_n=200; else files=(bench/small/*.bin); want_n=2000; fi
total=$(cat "${files[@]}" | wc -c 2>/dev/null) 
for run in $(seq 1 $runs); do
  rm -rf brecv bsend.out; mkdir brecv
  if [ $tool = old ]; then
    export SSL_CERT_FILE=$S/cert.pem
    (python3 ptyrun.py ./warpdrop-old send --domain localhost:8443 "${files[@]}" >bsend.out 2>&1) &
    for i in $(seq 1 200); do grep -qi "room id" bsend.out 2>/dev/null && break; sleep 0.1; done
    ROOM=$(strip < bsend.out | grep -o 'Room ID: [a-z-]*' | head -1 | cut -d' ' -f3)
    S0=$(date +%s.%N)
    python3 ptyrun.py ./warpdrop-old receive --domain localhost:8443 --dir brecv "$ROOM" >brecv.out 2>&1; rc=$?
    S1=$(date +%s.%N)
  else
    export WARPDROP_SERVER=http://127.0.0.1:18082
    sleep 7  # stay inside the new server's room-creation rate limit
    (./warpdrop send "${files[@]}" >bsend.out 2>&1) &
    for i in $(seq 1 200); do grep -q "Code:" bsend.out 2>/dev/null && break; sleep 0.1; done
    CODE=$(strip < bsend.out | grep -o '[0-9]\{4,6\}-[a-z]*-[a-z]*-[a-z]*-[a-z]*' | head -1)
    S0=$(date +%s.%N)
    ./warpdrop receive "$CODE" --yes --dir brecv >brecv.out 2>&1; rc=$?
    S1=$(date +%s.%N)
  fi
  sleep 1
  got_n=$(ls brecv | grep -vc '\.part$'); got_b=$(du -sb brecv | cut -f1)
  secs=$(echo "$S1 - $S0" | bc)
  mbps=$(echo "scale=1; $got_b / 1048576 / $secs" | bc)
  echo "$tool $set run$run: rc=$rc files=$got_n/$want_n bytes=$got_b time=${secs}s throughput=${mbps} MiB/s"
  pkill -x warpdrop-old 2>/dev/null; pkill -x warpdrop 2>/dev/null; sleep 1
done
