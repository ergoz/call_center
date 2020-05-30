package queue

import (
	"context"
	"fmt"
	"github.com/webitel/call_center/agent_manager"
	"github.com/webitel/call_center/call_manager"
	"github.com/webitel/call_center/mq"
	"github.com/webitel/call_center/store"
	"github.com/webitel/call_center/utils"
	"github.com/webitel/wlog"
	"sync"
	"time"
)

var DEFAULT_WATCHER_POLLING_INTERVAL = 500

type DialingImpl struct {
	app               App
	store             store.Store
	watcher           *utils.Watcher
	queueManager      *QueueManager
	resourceManager   *ResourceManager
	statisticsManager *StatisticsManager
	agentManager      agent_manager.AgentManager
	callManager       call_manager.CallManager
	startOnce         sync.Once
}

func NewDialing(app App, m mq.MQ, callManager call_manager.CallManager, agentManager agent_manager.AgentManager, s store.Store) Dialing {
	var dialing DialingImpl
	dialing.app = app
	dialing.store = s
	dialing.agentManager = agentManager
	dialing.resourceManager = NewResourceManager(app)
	dialing.statisticsManager = NewStatisticsManager(s)
	dialing.queueManager = NewQueueManager(app, s, m, callManager, dialing.resourceManager, agentManager)
	return &dialing
}

func (dialing *DialingImpl) Manager() *QueueManager {
	return dialing.queueManager
}

func (dialing *DialingImpl) Start() {
	wlog.Debug("starting dialing service")
	dialing.watcher = utils.MakeWatcher("Dialing", DEFAULT_WATCHER_POLLING_INTERVAL, dialing.routeData)

	dialing.startOnce.Do(func() {
		go dialing.watcher.Start()
		go dialing.queueManager.Start()
		go dialing.statisticsManager.Start()
	})
}

func (d *DialingImpl) Stop() {
	d.queueManager.Stop()
	d.watcher.Stop()
	d.statisticsManager.Stop()
}

func (d *DialingImpl) routeData() {
	d.routeIdleAttempts()
	d.routeIdleAgents()
}

func (d *DialingImpl) routeIdleAttempts() {
	if !d.app.IsReady() {
		return
	}

	if channels, err := d.store.Agent().GetChannelTimeout(); err == nil {
		for _, v := range channels {
			waiting := NewWaitingChannelEvent(v.Channel, v.UserId, nil, v.Timestamp)
			//FIXME QueueId ?
			err = d.queueManager.mq.AgentChannelEvent(v.Channel, v.DomainId, 0, v.UserId, waiting)
		}
	} else {
		wlog.Error(err.Error()) ///TODO return ?
	}

	members, err := d.store.Member().GetActiveMembersAttempt(d.app.GetInstanceId())
	if err != nil {
		wlog.Error(err.Error())
		time.Sleep(time.Second)
		return
	}

	for _, v := range members {
		att, _ := d.queueManager.CreateAttemptIfNotExists(context.Background(), v) //todo check err
		d.queueManager.input <- att
	}
}

func (d *DialingImpl) routeIdleAgents() {
	if !d.app.IsReady() {
		return
	}

	// FIXME engine
	if attempts, err := d.store.Member().GetTimeouts(d.app.GetInstanceId()); err == nil {
		for _, v := range attempts {
			if attempt, ok := d.queueManager.membersCache.Get(v.Id); ok {
				if _, err := d.queueManager.GetQueue(attempt.(*Attempt).QueueId(), attempt.(*Attempt).QueueUpdatedAt()); err == nil {
					attempt.(*Attempt).SetTimeout(v)
				} else {
					wlog.Error(fmt.Sprintf("Not found queue AttemptId=%d", v.Id))
				}
			} else {
				wlog.Error(fmt.Sprintf("Not found active attempt Id=%d", v.Id))
			}
		}
	} else {
		wlog.Error(err.Error()) ///TODO return ?
	}
	// FIXME engine
	if hists, err := d.store.Member().SaveToHistory(); err == nil {
		for _, h := range hists {
			wlog.Debug(fmt.Sprintf("Attempt=%d result %s", h.Id, h.Result))
		}
	}

	result, err := d.store.Agent().ReservedForAttemptByNode(d.app.GetInstanceId())
	if err != nil {
		wlog.Error(err.Error())
		time.Sleep(time.Second)
		return
	}

	if len(result) > 1 {
		fmt.Println(len(result))
	}
	for _, v := range result {
		agent, err := d.agentManager.GetAgent(v.AgentId, v.AgentUpdatedAt)
		if err != nil {
			wlog.Error(err.Error())
			continue
		}
		d.routeAgentToAttempt(v.AttemptId, agent)
	}
}

func (d *DialingImpl) routeAgentToAttempt(attemptId int64, agent agent_manager.AgentObject) {
	if attempt, ok := d.queueManager.membersCache.Get(attemptId); ok {

		if _, err := d.queueManager.GetQueue(int(attempt.(*Attempt).QueueId()), attempt.(*Attempt).QueueUpdatedAt()); err == nil {
			attempt.(*Attempt).DistributeAgent(agent)
		} else {
			wlog.Error(fmt.Sprintf("Not found queue AttemptId=%d for agent %s", attemptId, agent.Name()))
		}
	} else {
		wlog.Error(fmt.Sprintf("Not found active attempt Id=%d for agent %s", attemptId, agent.Name()))
	}
}
