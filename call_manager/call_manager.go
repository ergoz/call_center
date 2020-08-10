package call_manager

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/webitel/call_center/external_commands"
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/mq"
	"github.com/webitel/call_center/utils"
	"github.com/webitel/engine/discovery"
	"github.com/webitel/wlog"
	"net/http"
	"sync"
)

const (
	MAX_CALL_CACHE        = 10000
	MAX_CALL_EXPIRE_CACHE = 60 * 60 * 24 //day

	WATCHER_INTERVAL = 1000 * 5 // 30s
)

type CallManager interface {
	Start()
	Stop()
	ActiveCalls() int
	NewCall(callRequest *model.CallRequest) Call
	GetCall(id string) (Call, bool)
	InboundCall(call *model.Call, ringtone string) (Call, *model.AppError)
	CountConnection() int
	GetFlowUri() string
}

type CallManagerImpl struct {
	nodeId string

	serviceDiscovery discovery.ServiceDiscovery
	poolConnections  discovery.Pool

	flowSocketUri string

	mq        mq.MQ
	calls     utils.ObjectCache
	stop      chan struct{}
	stopped   chan struct{}
	watcher   *utils.Watcher
	proxy     string
	startOnce sync.Once
}

func NewCallManager(nodeId string, serviceDiscovery discovery.ServiceDiscovery, mq mq.MQ) CallManager {
	return &CallManagerImpl{
		nodeId:           nodeId,
		poolConnections:  discovery.NewPoolConnections(),
		serviceDiscovery: serviceDiscovery,
		mq:               mq,
		stop:             make(chan struct{}),
		stopped:          make(chan struct{}),
		calls:            utils.NewLruWithParams(MAX_CALL_CACHE, "CallManager", MAX_CALL_EXPIRE_CACHE, ""),
	}
}

func (cm *CallManagerImpl) Start() {
	wlog.Debug("starting call manager service")

	if services, err := cm.serviceDiscovery.GetByName(model.CLUSTER_CALL_SERVICE_NAME); err != nil {
		panic(err) //TODO
	} else {
		for _, v := range services {
			cm.registerConnection(v)
		}
	}

	cm.startOnce.Do(func() {
		cm.watcher = utils.MakeWatcher("CallManager", WATCHER_INTERVAL, cm.wakeUp)
		go cm.watcher.Start()
		go func() {
			defer func() {
				wlog.Debug("stopped CallManager")
				close(cm.stopped)
			}()

			for {
				select {
				case <-cm.stop:
					wlog.Debug("callManager received stop signal")
					return
				case e, ok := <-cm.mq.ConsumeCallEvent():
					if !ok {
						return
					}
					cm.handleCallAction(e)
				}
			}
		}()
	})
}

func (cm *CallManagerImpl) Stop() {
	wlog.Debug("callManager Stopping")

	if cm.watcher != nil {
		cm.watcher.Stop()
	}

	if cm.poolConnections != nil {
		cm.poolConnections.CloseAllConnections()
	}

	close(cm.stop)
	<-cm.stopped
}

func DUMP(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	wlog.Error(string(s))
	return string(s)
}

func (cm *CallManagerImpl) NewCall(callRequest *model.CallRequest) Call {
	api, _ := cm.getApiConnection() //FIXME!!! check error
	return NewCall(CALL_DIRECTION_OUTBOUND, callRequest, cm, api)
}

func (cm *CallManagerImpl) InboundCall(call *model.Call, ringtone string) (Call, *model.AppError) {
	cli, err := cm.getApiConnectionById(call.AppId)
	if err != nil {
		return nil, err
	}

	err = cli.JoinQueue(context.Background(), call.Id, ringtone, map[string]string{
		model.QUEUE_NODE_ID_FIELD: cm.nodeId,
		"cc_result":               "abandoned",
		//"bridge_export_vars":      "cc_agent_id",
		//"bridge_export_vars":      model.QUEUE_AGENT_ID_FIELD,
		//"transfer_after_bridge":   "'park:':inline",
		//"valet_hold_music":        play,
	})

	if err != nil {
		return nil, err
	}

	res := &CallImpl{
		callRequest: nil,
		direction:   model.CALL_DIRECTION_INBOUND, //FIXME
		id:          call.Id,
		api:         cli,
		cm:          cm,
		hangupCh:    make(chan struct{}),
		chState:     make(chan CallState, 5),
		acceptAt:    call.AnsweredAt,
		offeringAt:  call.CreatedAt,
		state:       CALL_STATE_ACCEPT, //FIXME
	}

	res.info = model.CallActionInfo{
		GatewayId:   nil,
		UserId:      nil,
		Direction:   "inbound",
		Destination: call.Destination,
		From: &model.CallEndpoint{
			Type:   "dest",
			Id:     call.FromNumber,
			Number: call.FromNumber,
			Name:   call.FromName,
		},
		To:       nil,
		ParentId: nil,
		Payload:  nil,
	}

	cm.saveToCacheCall(res)

	wlog.Debug(fmt.Sprintf("[%s] call %s init request", res.NodeName(), res.Id()))

	return res, nil
}

func (cm *CallManagerImpl) ActiveCalls() int {
	return cm.calls.Len()
}

func (cm *CallManagerImpl) GetCall(id string) (Call, bool) {
	if call, ok := cm.calls.Get(id); ok {
		return call.(Call), true
	}
	return nil, false
}

func (cm *CallManagerImpl) GetFlowUri() string {
	return "socket " + cm.flowSocketUri
}

func (cm *CallManagerImpl) registerConnection(v *discovery.ServiceConnection) {
	var version string
	var sps int
	client, err := external_commands.NewCallConnection(v.Id, fmt.Sprintf("%s:%d", v.Host, v.Port))
	if err != nil {
		wlog.Error(fmt.Sprintf("connection %s error: %s", v.Id, err.Error()))
		return
	}

	if version, err = client.GetServerVersion(); err != nil {
		wlog.Error(fmt.Sprintf("connection %s get version error: %s", v.Id, err.Error()))
		return
	}

	if sps, err = client.GetRemoteSps(); err != nil {
		wlog.Error(fmt.Sprintf("connection %s get SPS error: %s", v.Id, err.Error()))
		return
	}
	client.SetConnectionSps(sps)

	//FIXME add connection proxy value
	cm.proxy, err = client.GetParameter("outbound_sip_proxy")
	if err != nil {
		wlog.Error(fmt.Sprintf("connection %s get proxy error: %s", v.Id, err.Error()))
		return
	}

	if cm.flowSocketUri == "" {
		if cm.flowSocketUri, err = client.GetSocketUri(); err != nil {
			wlog.Error(fmt.Sprintf("connection %s get flow uri error: %s", v.Id, err.Error()))
			return
		}
	}

	cm.poolConnections.Append(client)
	wlog.Debug(fmt.Sprintf("register connection %s [%s] [sps=%d]", client.Name(), version, sps))
}

func (cm *CallManagerImpl) getApiConnection() (model.CallCommands, *model.AppError) {
	conn, err := cm.poolConnections.Get(discovery.StrategyRoundRobin)
	if err != nil {
		return nil, model.NewAppError("CallManager", "call_manager.get_client.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return conn.(model.CallCommands), nil
}

func (cm *CallManagerImpl) getApiConnectionById(id string) (model.CallCommands, *model.AppError) {
	conn, err := cm.poolConnections.GetById(id)
	if err != nil {
		return nil, model.NewAppError("CallManager", "call_manager.get_client.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return conn.(model.CallCommands), nil
}

func (cm *CallManagerImpl) wakeUp() {
	list, err := cm.serviceDiscovery.GetByName(model.CLUSTER_CALL_SERVICE_NAME)
	if err != nil {
		wlog.Error(err.Error())
		return
	}

	for _, v := range list {
		if _, err := cm.poolConnections.GetById(v.Id); err == discovery.ErrNotFoundConnection {
			cm.registerConnection(v)
		}
	}
	cm.poolConnections.RecheckConnections()
}

func (cm *CallManagerImpl) saveToCacheCall(call Call) {
	wlog.Debug(fmt.Sprintf("[%s] call %s save to store", call.NodeName(), call.Id()))
	cm.calls.AddWithDefaultExpires(call.Id(), call)
}

func (cm *CallManagerImpl) removeFromCacheCall(call Call) {
	wlog.Debug(fmt.Sprintf("[%s] call %s remove from store", call.NodeName(), call.Id()))
	cm.calls.Remove(call.Id())
}

func (cm *CallManagerImpl) CountConnection() int {
	return len(cm.poolConnections.All())
}
