#!/usr/bin/env bash

function init() {
    main_jrpc="http://localhost:7903"
    echo "=========== # start set wallet 1 ============="
    echo "=========== # save seed to wallet ============="
	result=$(./ycc-cli --rpc_laddr=${main_jrpc} seed generate -l 0)
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} seed save -p 1314fuzamei -s "${result}" | jq ".isok")
    if [ "${result}" = "false" ]; then
        echo "save seed to wallet error seed, result: ${result}"
        exit 1
    fi

    sleep 1

    echo "=========== # unlock wallet ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} wallet unlock -p 1314fuzamei -t 0 | jq ".isok")
    if [ "${result}" = "false" ]; then
        exit 1
    fi

    sleep 1

    echo "=========== # create new key for transfer ============="
    transfer_account=$(./ycc-cli --rpc_laddr=${main_jrpc} account create -l transfer | jq ".acc.addr"| sed -r 's/"//g')
    echo "${transfer_account}"
    if [ -z "${transfer_account}" ]; then
        exit 1
    fi

    echo "=========== # get transfer key ============="
    transfer_prikey=$(./ycc-cli --rpc_laddr=${main_jrpc} account dump_key -a ${transfer_account} | jq ".data" | sed -r 's/"//g')
    echo "${transfer_prikey}"
    if [ -z "${transfer_prikey}" ]; then
        exit 1
    fi

    sleep 1
    echo "=========== # create new key for mining ============="
    mining_account=$(./ycc-cli --rpc_laddr=${main_jrpc} account create -l mining | jq ".acc.addr" | sed -r 's/"//g')
    echo "${mining_account}"
    if [ -z "${mining_account}" ]; then
        exit 1
    fi
    sleep 1

    echo "=========== # get mining key ============="
    mining_prikey=$(./ycc-cli --rpc_laddr=${main_jrpc} account dump_key -a ${mining_account} | jq ".data" | sed -r 's/"//g')
    echo "${mining_prikey}"
    if [ -z "${mining_prikey}" ]; then
        exit 1
    fi

    sleep 1
    echo "=========== # send to transfer_account  ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} send coins transfer -a=100003000 -t ${transfer_account} -k CC38546E9E659D15E6B4893F0AB32A06D103931A8230B0BDE71459D2B27D6944)
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    echo "=========== # send to mining_account  ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} send coins transfer -a=100003000 -t ${mining_account} -k CC38546E9E659D15E6B4893F0AB32A06D103931A8230B0BDE71459D2B27D6944)
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    sleep 3
    echo "=========== # miner blsbind ============="
	result=$(./ycc-cli --rpc_laddr=${main_jrpc} pos33 blsbind)
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    sleep 1
    echo "=========== # transfer_account deposit to pos33  ============="
    deposit_hash=$( ./ycc-cli --rpc_laddr=${main_jrpc} send coins transfer -a=100000000 -t 1Wj2mPoBwJMVwAQLKPNDseGpDNibDt9Vq -k ${transfer_prikey})
    echo "${deposit_hash}"
    if [ -z "${deposit_hash}" ]; then
        exit 1
    fi
   
    sleep 1
    echo "=========== # transf_account entrust mining  ============="
    entrust_hash=$(./ycc-cli --rpc_laddr=${main_jrpc} send pos33 entrust -a 1000000 -e ${mining_account}  -r ${transfer_account} -k ${transfer_prikey})
    echo "${entrust_hash}"
    if [ -z "${entrust_hash}" ]; then
        exit 1
    fi

    echo "=========== # end  ============="

}

init
