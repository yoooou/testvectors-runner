/*
 * Copyright 2019-present Open Networking Foundation
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/*
Package p4rt implements p4runtime functions
*/
package p4rt

import (
	"bytes"
	"context"
	"time"

	v1 "github.com/abhilashendurthi/p4runtime/proto/p4/v1"

	"github.com/opennetworkinglab/testvectors-runner/pkg/logger"
	tg "github.com/stratum/testvectors/proto/target"
)

var log = logger.NewLogger()

//PktTimeout for receiving all packets
const PktTimeout = 3 * time.Second

var (
	scv      streamChannel
	p4rtConn connection
)

//Connection struct stores the P4Runtime client connection, context and cancel function.
type connection struct {
	ctx       context.Context
	client    v1.P4RuntimeClient
	connError error
	cancel    context.CancelFunc
}

//streamChannel struct stores stream channel client, cancel function and channels to receive stream messages
type streamChannel struct {
	sc                                   v1.P4Runtime_StreamChannelClient
	scError                              error
	cancel                               context.CancelFunc
	masterArbRecvChan, masterArbSendChan chan *v1.MasterArbitrationUpdate
	pktInChan                            chan *v1.PacketIn
	pktOutChan                           chan *v1.PacketOut
	genericStreamMessageChannel          chan *v1.StreamMessageResponse
}

//Init starts a P4Runtime client and runs go routines to send and receive stream channel messages from P4Runtime stream channel client
func Init(target *tg.Target) {
	log.Debug("In p4_oper Init")
	p4rtConn = connect(target)
	scv = getStreamChannel(p4rtConn.client)
}

//TearDown closes the stream channel client
func TearDown() {
	log.Debug("In p4_oper tear down")
	scv.cancel()
	if scv.sc != nil {
		err := scv.sc.CloseSend()
		if err != nil {
			log.Warn("Error closing the stream channel:", err)
		}
	}
	p4rtConn.cancel()
}

//ProcessP4WriteRequest sends the write request to switch
func ProcessP4WriteRequest(wreq *v1.WriteRequest, wres *v1.WriteResponse) bool {
	if wreq == nil {
		return false
	}

	lock := getMasterArbitrationLock(scv, wreq.DeviceId, wreq.ElectionId)

	if lock {
		log.Info("Sending P4 write request")
		log.Debugf("Write request: %s", wreq)
		ctx := context.Background()
		resp, err := p4rtConn.client.Write(ctx, wreq)
		if err != nil {
			log.Errorf("Error sending P4 write request:%v", err)
			return false
		}
		log.Infof("Received P4 write response")
		log.Debugf("P4 write response:%s", resp)
		return true
	}
	return false
}

//ProcessP4PipelineConfigOperation sends SetForwardingPipelineConfigRequest to switch
func ProcessP4PipelineConfigOperation(req *v1.SetForwardingPipelineConfigRequest, res *v1.SetForwardingPipelineConfigResponse) bool {
	if req == nil {
		return false
	}
	lock := getMasterArbitrationLock(scv, req.DeviceId, req.ElectionId)
	if lock {
		log.Info("Sending P4 pipeline config")
		log.Debugf("Pipeline config: %s", req)
		ctx := context.Background()
		resp, err := p4rtConn.client.SetForwardingPipelineConfig(ctx, req)
		if err != nil {
			log.Errorf("Error sending P4 pipeline config:%v", err)
			return false
		}
		log.Info("Received P4 pipeline config response")
		log.Debugf("P4 set pipeline config response:%s\n", resp)
		return true
	}
	return false
}

//ProcessPacketOutOperation sends packet to stream channel client.
func ProcessPacketOutOperation(po *v1.PacketOut) bool {
	var deviceID uint64 = 1
	electionID := &v1.Uint128{High: 1, Low: 5}
	lock := getMasterArbitrationLock(scv, deviceID, electionID)
	if lock {
		log.Info("Sending packet")
		log.Debugf("Packet info: %s", po)
		scv.pktOutChan <- po
		return true
	}
	return false
}

//ProcessPacketIn verifies if the packet received is same as expected packet.
func ProcessPacketIn(exp *v1.PacketIn) bool {
	packetMatched := false

	select {
	case ret := <-scv.pktInChan:
		log.Debug("In ProcessPacketIn Case PktInChan")
		if bytes.Equal(ret.GetPayload(), exp.GetPayload()) {
			packetMatched = true
			log.Infof("Received packet matches")
			log.Debugf("Packet info: %s", ret)
		} else {
			log.Warnf("Packets don't match\nExpected: % x\nActual  : % x\n", exp.GetPayload(), ret.GetPayload())
		}
		return packetMatched
	case <-time.After(PktTimeout):
		log.Error("Timed out waiting for packet in")
	}

	return packetMatched
}
