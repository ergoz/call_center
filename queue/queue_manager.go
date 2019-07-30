package queue

import (
	"fmt"
	"github.com/webitel/call_center/agent_manager"
	"github.com/webitel/call_center/call_manager"
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/store"
	"github.com/webitel/call_center/utils"
	"github.com/webitel/wlog"
	"sync"
)

const (
	MAX_QUEUES_CACHE        = 10000
	MAX_MEMBERS_CACHE       = 30000
	MAX_QUEUES_EXPIRE_CACHE = 60 * 60 * 24 //day
)

type QueueManager struct {
	wg              sync.WaitGroup
	app             App
	attemptCount    int64
	stop            chan struct{}
	stopped         chan struct{}
	input           chan *Attempt
	queuesCache     utils.ObjectCache
	membersCache    utils.ObjectCache
	store           store.Store
	resourceManager *ResourceManager
	agentManager    agent_manager.AgentManager
	callManager     call_manager.CallManager
	sync.Mutex
}

func NewQueueManager(app App, s store.Store, callManager call_manager.CallManager, resourceManager *ResourceManager, agentManager agent_manager.AgentManager) *QueueManager {
	return &QueueManager{
		store:           s,
		app:             app,
		callManager:     callManager,
		resourceManager: resourceManager,
		agentManager:    agentManager,
		input:           make(chan *Attempt),
		stop:            make(chan struct{}),
		stopped:         make(chan struct{}),
		queuesCache:     utils.NewLruWithParams(MAX_QUEUES_CACHE, "QueueManager", MAX_QUEUES_EXPIRE_CACHE, ""),
		membersCache:    utils.NewLruWithParams(MAX_MEMBERS_CACHE, "Members", MAX_QUEUES_EXPIRE_CACHE, ""),
	}
}

func (queueManager *QueueManager) Start() {
	wlog.Debug("QueueManager started")

	defer func() {
		wlog.Debug("Stopped QueueManager")
		close(queueManager.stopped)
	}()

	for {
		select {
		case <-queueManager.stop:
			wlog.Debug("QueueManager received stop signal")
			close(queueManager.input)
			return
		case attempt := <-queueManager.input:
			queueManager.DistributeAttempt(attempt)
		case call := <-queueManager.callManager.InboundCall():
			go queueManager.DistributeCall(call)
		}
	}
}

func (queueManager *QueueManager) Stop() {
	wlog.Debug("QueueManager Stopping")
	wlog.Debug(fmt.Sprintf("Wait for close attempts %d", queueManager.membersCache.Len()))
	queueManager.wg.Wait()
	close(queueManager.stop)
	<-queueManager.stopped
}

func (queueManager *QueueManager) GetNodeId() string {
	return queueManager.app.GetInstanceId()
}

func (queueManager *QueueManager) RouteMember(attempt *model.MemberAttempt) {
	var a *Attempt
	var ok bool

	if a, ok = queueManager.GetAttempt(attempt.Id); ok {

		if attempt.Result == nil {
			wlog.Error(fmt.Sprintf("Attempt %v in queue", a.Id()))
		} else {
			a.SetMember(attempt)
		}
		return
	} else {
		a = queueManager.NewAttempt(attempt)
	}

	queueManager.input <- a
}

func (queueManager *QueueManager) NewAttempt(conf *model.MemberAttempt) *Attempt {

	attempt := NewAttempt(conf)
	queueManager.membersCache.AddWithDefaultExpires(attempt.Id(), attempt)
	queueManager.wg.Add(1)
	queueManager.attemptCount++

	wlog.Debug(fmt.Sprintf("new attempt %d", attempt.Id()))

	return attempt
}

func (queueManager *QueueManager) GetQueue(id int, updatedAt int64) (QueueObject, *model.AppError) {
	queueManager.Lock()
	defer queueManager.Unlock()
	var queue QueueObject

	item, ok := queueManager.queuesCache.Get(id)
	if ok {
		queue, ok = item.(QueueObject)
		if ok && !queue.IsExpire(updatedAt) {
			return queue, nil
		}
	}

	if config, err := queueManager.app.GetQueueById(int64(id)); err != nil {
		return nil, err
	} else {
		queue, err = NewQueue(queueManager, queueManager.resourceManager, config)
		if err != nil {
			return nil, err
		}
	}

	queueManager.queuesCache.AddWithDefaultExpires(id, queue)
	wlog.Debug(fmt.Sprintf("Add queue %s to cache", queue.Name()))
	return queue, nil
}

func (queueManager *QueueManager) GetResource(id, updatedAt int64) (ResourceObject, *model.AppError) {
	return queueManager.resourceManager.Get(id, updatedAt)
}

func (queueManager *QueueManager) SetResourceError(resource ResourceObject, routingId int, errorId string) {
	if resource.CheckIfError(errorId) {
		wlog.Warn(fmt.Sprintf("Resource %s Id=%d error: %s", resource.Name(), resource.Id(), errorId))
		if responseError, err := queueManager.store.OutboundResource().
			SetError(int64(resource.Id()), int64(routingId), errorId, model.OUTBOUND_RESOURCE_STRATEGY_RANDOM); err != nil {

			wlog.Error(err.Error())
		} else {
			if responseError.Stopped != nil && *responseError.Stopped {
				wlog.Info(fmt.Sprintf("Resource %s [%d] stopped, because: %s", resource.Name(), resource.Id(), errorId))

				queueManager.notifyStoppedResource(resource)
			}

			if responseError.UnReserveResourceId != nil {
				wlog.Info(fmt.Sprintf("New resource ResourceId=%d from reserve", *responseError.UnReserveResourceId))
			}
			queueManager.resourceManager.RemoveFromCacheById(int64(resource.Id()))
		}
	}
}

func (queueManager *QueueManager) SetResourceSuccessful(resource ResourceObject) {
	if resource.SuccessivelyErrors() > 0 {
		if err := queueManager.store.OutboundResource().SetSuccessivelyErrorsById(int64(resource.Id()), 0); err != nil {
			wlog.Error(err.Error())
		}
	}
}

func (queueManager *QueueManager) DistributeAttempt(attempt *Attempt) {

	queue, err := queueManager.GetQueue(int(attempt.QueueId()), attempt.QueueUpdatedAt())
	if err != nil {
		wlog.Error(err.Error())
		//TODO added to model "dialing.queue.new_queue.app_error"
		queueManager.SetAttemptMinus(attempt, model.MEMBER_CAUSE_QUEUE_NOT_IMPLEMENT)
		return
	}

	if attempt.IsBarred() {
		queueManager.attemptBarred(attempt, queue)
		return
	}

	if attempt.IsTimeout() {
		panic("CHANGE TO SET MEMBER FUNCTION")
		return
	}

	attempt.resource = queueManager.GetAttemptResource(attempt)
	queue.DistributeAttempt(attempt)
	queueManager.notifyChangedQueueLength(queue)

	wlog.Debug(fmt.Sprintf("join member %s[%d] AttemptId=%d to queue \"%s\"", attempt.Name(),
		attempt.MemberId(), attempt.Id(), queue.Name()))
}

func (queueManager *QueueManager) DistributeCall(call call_manager.Call) {
	var queueId, priority int
	var ok bool

	queueId, ok = call.GetIntAttribute(model.QUEUE_ID_FIELD)
	if !ok {
		wlog.Warn(fmt.Sprintf("[%s] call %s distribute to queue error: not found variable %s", call.NodeName(), call.Id(), model.QUEUE_ID_FIELD))
		return
	}

	priority, _ = call.GetIntAttribute(model.QUEUE_MEMBER_PRIORITY)

	attemptId, err := queueManager.store.Member().DistributeCallToQueue(int64(queueId), call.Id(), call.FromNumber(), call.FromName(), priority)

	if err != nil {
		wlog.Error(fmt.Sprintf("[%s] call %s distribute error: %s", call.NodeName(), call.Id(), err.Error()))
		return
	}

	wlog.Debug(fmt.Sprintf("[%s] call %s distributed, AttemptId=%d", call.NodeName(), call.Id(), attemptId))
}

func (queueManager *QueueManager) LeavingMember(attempt *Attempt, queue QueueObject) {
	queueManager.membersCache.Remove(attempt.Id())
	queueManager.notifyChangedQueueLength(queue) //TODO
	queueManager.wg.Done()

	wlog.Debug(fmt.Sprintf("leaving member %s[%d] AttemptId=%d from queue \"%s\"", attempt.Name(), attempt.MemberId(), attempt.Id(), queue.Name()))
}

func (queueManager *QueueManager) GetAttemptResource(attempt *Attempt) ResourceObject {
	if attempt.ResourceId() != nil && attempt.ResourceUpdatedAt() != nil {
		resource, err := queueManager.resourceManager.Get(*attempt.ResourceId(), *attempt.ResourceUpdatedAt())
		if err != nil {
			wlog.Error(fmt.Sprintf("attempt resource error: %s", err.Error()))
		} else {
			return resource
		}
	}
	return nil
}

func (queueManager *QueueManager) GetAttempt(id int64) (*Attempt, bool) {
	if attempt, ok := queueManager.membersCache.Get(id); ok {
		return attempt.(*Attempt), true
	}

	return nil, false
}

func (queueManager *QueueManager) attemptBarred(attempt *Attempt, queue QueueObject) {
	if stopped, err := queueManager.SetAttemptBarred(attempt); err != nil {
		wlog.Error(err.Error())
	} else {
		wlog.Warn(fmt.Sprintf("barred member %s[%d] Destination=\"%v\" AttemptId=%d in queue \"%s\"", attempt.Name(), attempt.MemberId(),
			attempt.Destination(), attempt.Id(), queue.Name()))
		queueManager.notifyStopAttempt(attempt, stopped)
	}
}
