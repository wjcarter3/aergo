#!/usr/bin/env bash

source set_test_env.sh
source test_common.sh

kill_svr.sh

if [ ! -e $TEST_RAFT_INSTANCE/BP11001.toml ];then
	prepareConfig 3
fi

if [ "$TEST_SKIP_GENESIS" = "1" ];then
	echo "================ skip init genesis node and reboot aergosvr ===========+++=="
	run_svr.sh
	sleep 3
	exit 0
fi

pushd $TEST_RAFT_INSTANCE

clean.sh
rm init_*.log


if [ $# != 0 ]; then
    echo "Usage: $0"
    exit
fi


rm -rf genesis
rm -f genesis.json

for file in BP*.toml; do
    bpname=${file%%.toml}
    echo "./init_genesis.sh $bpname"
#init_genesis.sh $bpname > /dev/null 2>&1
    init_genesis.sh $bpname 
done

popd
