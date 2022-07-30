#!/bin/sh

ulimit -c unlimited
ulimit -n 10240

monitor() {
  proc_name=$1
  agrs="$2"
  proc_dir=${proc_name%/*}
  proc_bin=./${proc_name##*/}
  #echo "$proc_dir"
  #echo "$proc_bin"
  #echo "$agrs"
  cd "${proc_dir}" || exit
  # shellcheck disable=SC2006
  DATE=$(date "+%Y%m%d%H%M%S")
  echo "[$DATE] start monitor proc[$proc_name] ..."

  proc_num=$(ps -ax | grep -v grep | grep -c "$proc_bin")
  if [ "$proc_num" -eq 0 ]
  then
  	DATE=$(date)
  	echo "[$DATE] proc[$proc_name] dead, do restart..."
  	"${proc_bin}" "${agrs}" >> "${proc_dir}"/"${proc_bin}".log 2>&1 &
  fi


  return 0
}

monitor /root/vps-client/gost-linux-amd64 -C=/root/vps-client/config/vps.json
