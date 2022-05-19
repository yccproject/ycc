#!/usr/bin/env bash

function init() {
    main_jrpc="http://localhost:9901"
    echo "=========== # start set wallet 1 ============="
    echo "=========== # save seed to wallet ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} seed save -p 1314fuzamei -s "tortoise main civil member grace happy century convince father cage beach hip maid merry rib" | jq ".isok")
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

    echo "=========== # import private key transfer ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} account import_key -t 2 -k CC38546E9E659D15E6B4893F0AB32A06D103931A8230B0BDE71459D2B27D6944 -l transfer | jq ".label")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    sleep 1

    echo "=========== # import private key mining ============="
    result=$(./ycc-cli --rpc_laddr=${main_jrpc} account import_key -t 2 -k 4257D8692EF7FE13C68B65D6A52F03933DB2FA5CE8FAF210B5B8B80C721CED01 -l mining | jq ".label")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

}

init
