#!/usr/bin/env bash

function init() {
    echo "=========== # start set wallet 1 ============="
    echo "=========== # save seed to wallet ============="
    result=$(./ycc-cli seed generate -l 0)
    result=$(./ycc-cli seed save -p 1314fuzamei -s "${result}" | jq ".isok")
    if [ "${result}" = "false" ]; then
        echo "save seed to wallet error seed, result: ${result}"
        exit 1
    fi

    sleep 1

    echo "=========== # unlock wallet ============="
    result=$(./ycc-cli wallet unlock -p 1314fuzamei -t 0 | jq ".isok")
    if [ "${result}" = "false" ]; then
        exit 1
    fi

    sleep 1

    echo "=========== # create new key for transfer ============="
    result=$(./ycc-cli account create -l transfer | jq ".acc")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    # sleep 1
    # echo "=========== # deposit to pos33 ============="
    # result=$(./ycc-cli send coins transfer -a=1000000000 -t=1Wj2mPoBwJMVwAQLKPNDseGpDNibDt9Vq -k=transfer | jq "")
    # echo "${result}"
    # if [ -z "${result}" ]; then
    #     exit 1
    # fi

    sleep 1
    echo "=========== # create new key for mining ============="
    result=$(./ycc-cli account create -l mining | jq ".acc")
    echo "${result}"
    if [ -z "${result}" ]; then
        exit 1
    fi

    sleep 1
    echo "=========== # set auto mining ============="
    result=$(./ycc-cli pos33 auto_mine -f 1 | jq ".isok")
    if [ "${result}" = "false" ]; then
        exit 1
    fi

    echo "=========== # end set wallet 1 ============="

}

init
