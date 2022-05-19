#!/usr/bin/env bash

function init() {
    main_jrpc="http://localhost:9901"
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
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} account create -t 2 -l transfer | jq ".acc")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi


    sleep 1
    echo "=========== # create new key for mining ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} account create -t 2 -l mining | jq ".acc")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    echo "=========== # end set wallet 1 ============="

}

init
