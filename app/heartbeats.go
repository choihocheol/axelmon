package app

import (
	"context"
	"errors"
	"fmt"

	"bharvest.io/axelmon/client/grpc"
	"bharvest.io/axelmon/client/rpc"
	"bharvest.io/axelmon/log"
	"bharvest.io/axelmon/server"
	"bharvest.io/axelmon/tg"
	rewardTypes "github.com/axelarnetwork/axelar-core/x/reward/types"
	tssTypes "github.com/axelarnetwork/axelar-core/x/tss/types"
)

func (c *Config) checkHeartbeats(ctx context.Context) error {
	clientGRPC := grpc.New(c.General.GRPC)
	err := clientGRPC.Connect(ctx, c.General.GRPCSecureConnection)
	defer clientGRPC.Terminate(ctx)
	if err != nil {
		return err
	}

	heartbeatHeight, err := c.findHeartBeatHeight(ctx)
	if err != nil {
		return err
	}

	missCnt := 0
	log.Info(fmt.Sprintf("Broadcaster: %s", c.Wallet.Proxy.PrintAcc()))
	for i := 0; i < c.Heartbeat.CheckN; i++ {
		isFound, err := c.findHeartbeat(ctx, clientGRPC, heartbeatHeight, 5)
		if err != nil {
			return err
		}
		heartbeatHeight -= 50

		if !isFound {
			missCnt++
		}
	}

	server.GlobalState.Heartbeat.Missed = fmt.Sprintf("%d / %d", missCnt, c.Heartbeat.CheckN)
	if missCnt >= c.Heartbeat.MissCnt {
		server.GlobalState.Heartbeat.Status = false

		tg.SendMsg("Heartbeat status: 🛑")
		log.Info("Heartbeat status: 🛑")
	} else {
		server.GlobalState.Heartbeat.Status = true

		log.Info("Heartbeat status: 🟢")
	}

	return nil
}

func (c *Config) findHeartbeat(ctx context.Context, clientGRPC *grpc.Client, heartbeatHeight int64, tryCnt int) (bool, error) {
	for j := 0; j < tryCnt; j++ {
		log.Info(fmt.Sprintf("Search heartbeat on height: %d", heartbeatHeight))

		txs, err := clientGRPC.GetTxs(ctx, heartbeatHeight)
		if err != nil {
			return false, err
		}
		for _, tx := range txs {
			for _, msg := range tx.Body.Messages {
				if msg.TypeUrl == "/axelar.reward.v1beta1.RefundMsgRequest" {
					refundMsg := rewardTypes.RefundMsgRequest{}
					err = refundMsg.Unmarshal(msg.Value)
					if err != nil {
						return false, err
					}
					if refundMsg.InnerMessage.TypeUrl == "/axelar.tss.v1beta1.HeartBeatRequest" {
						heartbeat := tssTypes.HeartBeatRequest{}
						err = heartbeat.Unmarshal(refundMsg.InnerMessage.Value)
						if err != nil {
							return false, err
						}
						if heartbeat.Sender.Equals(c.Wallet.Proxy.Acc) && len(heartbeat.KeyIDs) >= 1 {
							log.Info(fmt.Sprintf("Found and the number of signed: %d", len(heartbeat.KeyIDs)))
							return true, nil
						}
					}
				}
			}
		}
		heartbeatHeight++
	}

	return false, errors.New(fmt.Sprintf("Didn't heartbeat signal on height: %d", heartbeatHeight))
}

func (c *Config) findHeartBeatHeight(ctx context.Context) (int64, error) {
	client, err := rpc.New(c.General.RPC)
	if err != nil {
		return 0, err
	}

	height, err := client.GetLatestHeight(ctx)
	if err != nil {
		return 0, err
	}

	var heartbeatHeight int64
	if height%50 != 0 {
		heartbeatHeight = height - (height % 50) + 1
	} else {
		heartbeatHeight = heartbeatHeight - 50 + 1
	}

	return heartbeatHeight, nil
}
