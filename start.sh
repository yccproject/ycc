#!/usr/bin/env bash

transfer_account=$1
miner_account=$2
amount=$3

function init() {
    main_jrpc="http://localhost:9901"
    echo "=========== # miner blsbind ============="
	result=$(./ycc-cli --rpc_laddr=${main_jrpc} pos33 blsbind)
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    sleep 1
    echo "=========== # transfer_account deposit to pos33  ============="
    deposit_hash=$( ./ycc-cli --rpc_laddr=${main_jrpc} send coins transfer -a $amount -t 0x79a69527dd0d62dcfd3eae90eb6c3134c57489eb -k $transfer_account)
    echo "${deposit_hash}"
    if [ -z "${deposit_hash}" ]; then
        exit 1
    fi
   
    sleep 1
    echo "=========== # transf_account entrust mining  ============="
    entrust_hash=$(./ycc-cli --rpc_laddr=${main_jrpc} send pos33 entrust -a $amount -e $miner_account -r $transfer_account -k $transfer_account)
    echo "${entrust_hash}"
    if [ -z "${entrust_hash}" ]; then
        exit 1
    fi
}

init
