package sqlstore

import (
	"encoding/json"
	"fmt"
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/store"
	"net/http"
)

type SqlMemberStore struct {
	SqlStore
}

func NewSqlMemberStore(sqlStore SqlStore) store.MemberStore {
	us := &SqlMemberStore{sqlStore}
	return us
}

func (s *SqlMemberStore) CreateTableIfNotExists() {

}

func (s SqlMemberStore) ReserveMembersByNode(nodeId string) (int64, *model.AppError) {
	if i, err := s.GetMaster().SelectNullInt(`call call_center.cc_distribute(null)`); err != nil {
		return 0, model.NewAppError("SqlMemberStore.ReserveMembers", "store.sql_member.reserve_member_resources.app_error",
			map[string]interface{}{"Error": err.Error()},
			err.Error(), http.StatusInternalServerError)
	} else {
		return i.Int64, nil
	}
}

func (s SqlMemberStore) UnReserveMembersByNode(nodeId, cause string) (int64, *model.AppError) {
	if i, err := s.GetMaster().SelectInt(`select s as count
			from call_center.cc_un_reserve_members_with_resources($1, $2) s`, nodeId, cause); err != nil {
		return 0, model.NewAppError("SqlMemberStore.UnReserveMembers", "store.sql_member.un_reserve_member_resources.app_error",
			map[string]interface{}{"Error": err.Error()}, err.Error(), http.StatusInternalServerError)
	} else {
		return i, nil
	}
}

func (s SqlMemberStore) GetActiveMembersAttempt(nodeId string) ([]*model.MemberAttempt, *model.AppError) {
	var members []*model.MemberAttempt
	if _, err := s.GetMaster().Select(&members, `select *
			from call_center.cc_set_active_members($1) s`, nodeId); err != nil {
		return nil, model.NewAppError("SqlMemberStore.GetActiveMembersAttempt", "store.sql_member.get_active.app_error",
			map[string]interface{}{"Error": err.Error()},
			err.Error(), http.StatusInternalServerError)
	} else {
		return members, nil
	}
}

func (s SqlMemberStore) SetAttemptState(id int64, state int) *model.AppError {
	if _, err := s.GetMaster().Exec(`update call_center.cc_member_attempt
			set state = :State
			where id = :Id`, map[string]interface{}{"Id": id, "State": state}); err != nil {
		return model.NewAppError("SqlMemberStore.SetAttemptState", "store.sql_member.set_attempt_state.app_error", nil,
			fmt.Sprintf("Id=%v, %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s SqlMemberStore) SetAttemptFindAgent(id int64) *model.AppError {
	if _, err := s.GetMaster().Exec(`update call_center.cc_member_attempt
			set state = :State,
				agent_id = null,
				team_id = null
			where id = :Id and state != :CancelState and result isnull`, map[string]interface{}{
		"Id":          id,
		"State":       model.MemberStateWaitAgent,
		"CancelState": model.MemberStateCancel,
	}); err != nil {
		return model.NewAppError("SqlMemberStore.SetFindAgentState", "store.sql_member.set_attempt_state_find_agent.app_error", nil,
			fmt.Sprintf("Id=%v, %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s SqlMemberStore) AnswerPredictAndFindAgent(id int64) *model.AppError {
	if _, err := s.GetMaster().Exec(`update call_center.cc_member_attempt
			set state = :State,
				agent_id = null,
				answered_at = now()
			where id = :Id and state != :CancelState and result isnull`, map[string]interface{}{
		"Id":          id,
		"State":       model.MemberStateWaitAgent,
		"CancelState": model.MemberStateCancel,
	}); err != nil {
		return model.NewAppError("SqlMemberStore.AnswerPredictAndFindAgent", "store.sql_member.set_attempt_answer_find_agent.app_error", nil,
			fmt.Sprintf("Id=%v, %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s SqlMemberStore) SetDistributeCancel(id int64, description string, nextDistributeSec uint32, stop bool, vars map[string]string) *model.AppError {
	_, err := s.GetMaster().Exec(`call call_center.cc_attempt_distribute_cancel(:Id::int8, :Desc::varchar, :NextSec::int4, :Stop::bool, :Vars::jsonb)`,
		map[string]interface{}{
			"Id":      id,
			"Desc":    description,
			"NextSec": nextDistributeSec,
			"Stop":    stop,
			"Vars":    nil,
		})

	if err != nil {
		return model.NewAppError("SqlMemberStore.SetDistributeCancel", "store.sql_member.set_distribute_cancel.app_error", nil,
			fmt.Sprintf("Id=%v, %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s SqlMemberStore) DistributeCallToQueue(node string, queueId int64, callId string, vars map[string]string, bucketId *int32, priority int, stickyAgentId *int) (*model.InboundCallQueue, *model.AppError) {
	var att *model.InboundCallQueue
	err := s.GetMaster().SelectOne(&att, `select *
from call_center.cc_distribute_inbound_call_to_queue(:AppId::varchar, :QueueId::int8, :CallId::varchar, :Variables::jsonb,
	:BucketId::int, :Priority::int, :StickyAgentId::int4)
as x (
    attempt_id int8,
    queue_id int,
    queue_updated_at int8,
    destination jsonb,
    variables jsonb,
    name varchar,
    team_updated_at int8,

    call_id varchar,
    call_state varchar,
    call_direction varchar,
    call_destination varchar,
    call_timestamp int8,
    call_app_id varchar,
    call_from_number varchar,
    call_from_name varchar,
    call_answered_at int8,
    call_bridged_at int8,
    call_created_at int8
);`, map[string]interface{}{
		"AppId":         node,
		"QueueId":       queueId,
		"CallId":        callId,
		"Variables":     model.MapToJson(vars),
		"BucketId":      bucketId,
		"Priority":      priority,
		"StickyAgentId": stickyAgentId,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.DistributeCallToQueue", "store.sql_member.distribute_call.app_error", nil,
			fmt.Sprintf("QueueId=%v, CallId=%v %s", queueId, callId, err.Error()), http.StatusInternalServerError)
	}

	return att, nil
}

func (s SqlMemberStore) DistributeCallToAgent(node string, callId string, vars map[string]string, agentId int32, force bool) (*model.InboundCallAgent, *model.AppError) {
	var att *model.InboundCallAgent

	err := s.GetMaster().SelectOne(&att, `select *
from call_center.cc_distribute_inbound_call_to_agent(:Node, :MemberCallId, :Variables, :AgentId)
as x (
    attempt_id int8,
    destination jsonb,
    variables jsonb,
    name varchar,
    team_id int,
    team_updated_at int8,
    agent_updated_at int8,

    call_id varchar,
    call_state varchar,
    call_direction varchar,
    call_destination varchar,
    call_timestamp int8,
    call_app_id varchar,
    call_from_number varchar,
    call_from_name varchar,
    call_answered_at int8,
    call_bridged_at int8,
    call_created_at int8
)
where :Force::bool or not exists(select 1 from call_center.cc_member_attempt a where a.agent_id = :AgentId and a.state != 'leaving' for update )`, map[string]interface{}{
		"Node":         node,
		"MemberCallId": callId,
		"Variables":    model.MapToJson(vars),
		"AgentId":      agentId,
		"Force":        force,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.DistributeCallToAgent", "store.sql_member.distribute_call_agent.app_error", nil,
			fmt.Sprintf("AgentId=%v, CallId=%v %s", agentId, callId, err.Error()), http.StatusInternalServerError)
	}

	return att, nil
}

func (s SqlMemberStore) DistributeCallToQueueCancel(id int64) *model.AppError {
	_, err := s.GetMaster().Exec(`update call_center.cc_member_attempt
set result = 'cancel',
    state = 'leaving',
    leaving_at = now()
where id = :Id`, map[string]interface{}{
		"Id": id,
	})

	if err != nil {

		return model.NewAppError("SqlMemberStore.DistributeCallToQueue2Cancel", "store.sql_member.distribute_call_cancel.app_error", nil,
			fmt.Sprintf("Id=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s SqlMemberStore) DistributeChatToQueue(node string, queueId int64, convId string, vars map[string]string, bucketId *int32, priority int, stickyAgentId *int) (*model.InboundChatQueue, *model.AppError) {
	var attempt *model.InboundChatQueue

	var v *string
	if vars != nil {
		v = new(string)
		*v = model.MapToJson(vars)
	}

	if err := s.GetMaster().SelectOne(&attempt, `select *
		from call_center.cc_distribute_inbound_chat_to_queue(:AppId::varchar, :QueueId::int8, :ConvId::varchar, :Variables::jsonb,
	:BucketId::int, :Priority::int, :StickyAgentId::int) 
as x (
    attempt_id int8,
    queue_id int,
    queue_updated_at int8,
    destination jsonb,
    variables jsonb,
    name varchar,
    team_updated_at int8,

    conversation_id varchar,
    conversation_created_at int8
);`,
		map[string]interface{}{
			"AppId":         node,
			"QueueId":       queueId,
			"ConvId":        convId,
			"Variables":     v,
			"BucketId":      bucketId,
			"Priority":      priority,
			"StickyAgentId": stickyAgentId,
		}); err != nil {
		return nil, model.NewAppError("SqlMemberStore.DistributeChatToQueue", "store.sql_member.distribute_chat.app_error", nil,
			fmt.Sprintf("QueueId=%v, Id=%v %s", queueId, convId, err.Error()), http.StatusInternalServerError)
	}

	return attempt, nil
}

func (s SqlMemberStore) DistributeDirect(node string, memberId int64, communicationId, agentId int) (*model.MemberAttempt, *model.AppError) {
	var res *model.MemberAttempt
	err := s.GetMaster().SelectOne(&res, `select * from call_center.cc_distribute_direct_member_to_queue(:AppId, :MemberId, :CommunicationId, :AgentId)`,
		map[string]interface{}{
			"AppId":           node,
			"MemberId":        memberId,
			"AgentId":         agentId,
			"CommunicationId": communicationId,
		})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.DistributeDirect", "store.sql_member.distribute_direct.app_error", nil,
			fmt.Sprintf("MemberId=%v, AgentId=%v %s", memberId, agentId, err.Error()), http.StatusInternalServerError)
	}

	return res, nil

}

func (s *SqlMemberStore) SetAttemptOffering(attemptId int64, agentId *int, agentCallId, memberCallId *string, destination, display *string) (int64, *model.AppError) {
	timestamp, err := s.GetMaster().SelectInt(`select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp"
from call_center.cc_attempt_offering(:AttemptId::int8, :AgentId::int4, :AgentCallId::varchar, :MemberCallId::varchar, :Dest::varchar, :Displ::varchar)
    as x (last_state_change timestamptz)
where x.last_state_change notnull `, map[string]interface{}{
		"AttemptId":    attemptId,
		"AgentId":      agentId,
		"AgentCallId":  agentCallId,
		"MemberCallId": memberCallId,
		"Dest":         destination,
		"Displ":        display,
	})

	if err != nil {
		return 0, model.NewAppError("SqlMemberStore.SetAttemptOffering", "store.sql_member.set_attempt_offering.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return timestamp, nil
}

func (s *SqlMemberStore) SetAttemptBridged(attemptId int64) (int64, *model.AppError) {
	timestamp, err := s.GetMaster().SelectInt(`select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp"
from call_center.cc_attempt_bridged(:AttemptId)
    as x (last_state_change timestamptz)
where x.last_state_change notnull `, map[string]interface{}{
		"AttemptId": attemptId,
	})

	if err != nil {
		return 0, model.NewAppError("SqlMemberStore.SetAttemptBridged", "store.sql_member.set_attempt_bridged.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return timestamp, nil
}

func (s *SqlMemberStore) SetAttemptAbandoned(attemptId int64) (*model.AttemptLeaving, *model.AppError) {
	var res *model.AttemptLeaving
	err := s.GetMaster().SelectOne(&res, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", x.member_stop_cause
from call_center.cc_attempt_abandoned(:AttemptId)
    as x (last_state_change timestamptz, member_stop_cause varchar)
where x.last_state_change notnull `, map[string]interface{}{
		"AttemptId": attemptId,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SetAttemptAbandoned", "store.sql_member.set_attempt_abandoned.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return res, nil
}

func mapToJson(m map[string]string) *string {
	if m == nil {
		return nil
	}

	if data, err := json.Marshal(m); err == nil {
		return model.NewString(string(data))
	}

	return nil
}

func (s *SqlMemberStore) SetAttemptAbandonedWithParams(attemptId int64, maxAttempts uint, sleep uint64, vars map[string]string) (*model.AttemptLeaving, *model.AppError) {
	var res *model.AttemptLeaving
	err := s.GetMaster().SelectOne(&res, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", x.member_stop_cause
from call_center.cc_attempt_abandoned(:AttemptId, :MaxAttempts, :Sleep, :Vars::jsonb)
    as x (last_state_change timestamptz, member_stop_cause varchar)
where x.last_state_change notnull `, map[string]interface{}{
		"AttemptId":   attemptId,
		"MaxAttempts": maxAttempts,
		"Sleep":       sleep,
		"Vars":        mapToJson(vars),
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SetAttemptAbandonedWithParams", "store.sql_member.set_attempt_abandoned.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return res, nil
}

func (s *SqlMemberStore) SetAttemptMissedAgent(attemptId int64, agentHoldSec int) (*model.MissedAgent, *model.AppError) {
	var res *model.MissedAgent
	err := s.GetMaster().SelectOne(&res, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", no_answers
from call_center.cc_attempt_missed_agent(:AttemptId, :AgentHoldSec)
    as x (last_state_change timestamptz, no_answers int)
where x.last_state_change notnull `, map[string]interface{}{
		"AttemptId":    attemptId,
		"AgentHoldSec": agentHoldSec,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SetAttemptMissedAgent", "store.sql_member.set_attempt_messed_agent.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return res, nil
}

func (s *SqlMemberStore) SetAttemptReporting(attemptId int64, deadlineSec uint32) (int64, *model.AppError) {
	timestamp, err := s.GetMaster().SelectInt(`with att as (
    update call_center.cc_member_attempt
    set timeout  = case when :DeadlineSec::int > 0 then  now() + (:DeadlineSec::int || ' sec')::interval end,
        leaving_at = now(),
	    last_state_change = now(),
        state = case when state <> 'leaving' then :State else state end
    where id = :Id
    returning agent_id, channel, state, leaving_at
)
update call_center.cc_agent_channel c
set state = att.state,
    joined_at = att.leaving_at
from att
where (att.agent_id, att.channel) = (c.agent_id, c.channel)
returning call_center.cc_view_timestamp(c.joined_at) as timestamp`, map[string]interface{}{
		"State":       model.ChannelStateProcessing,
		"Id":          attemptId,
		"DeadlineSec": deadlineSec,
	})

	if err != nil {
		return 0, model.NewAppError("SqlMemberStore.SetAttemptReporting", "store.sql_member.set_attempt_reporting.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attemptId, err.Error()), http.StatusInternalServerError)
	}

	return timestamp, nil
}

// fixme queue_id
func (s *SqlMemberStore) RenewalProcessing(domainId, attId int64, renewalSec uint32) (*model.RenewalProcessing, *model.AppError) {
	var res *model.RenewalProcessing
	err := s.GetMaster().SelectOne(&res, `update call_center.cc_member_attempt a
 set timeout = now() + (:Renewal::int || ' sec')::interval
from call_center.cc_member_attempt a2
    inner join call_center.cc_agent ca on ca.id = a2.agent_id
    left join call_center.cc_queue cq on cq.id = a2.queue_id
where a2.id = :Id::int8
	and a2.id = a.id
	and (cq.id isnull or cq.processing_renewal_sec > 0)
    and ca.domain_id = :DomainId::int8
	and a2.state = 'processing'
returning
    a.id attempt_id,
	coalesce(a.queue_id,0) as queue_id,
    call_center.cc_view_timestamp(a.timeout) timeout,
    call_center.cc_view_timestamp(now()) "timestamp",
	coalesce(cq.processing_renewal_sec, (:Renewal::int / 2)::int) as renewal_sec,
    a.channel,
    ca.user_id,
    ca.domain_id`, map[string]interface{}{
		"DomainId": domainId,
		"Id":       attId,
		"Renewal":  renewalSec,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.RenewalProcessing", "store.sql_member.set_attempt_renewal.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", attId, err.Error()), extractCodeFromErr(err))
	}

	return res, nil
}

func (s *SqlMemberStore) SetAttemptMissed(id int64, agentHoldTime int, maxAttempts uint, waitBetween uint64) (*model.MissedAgent, *model.AppError) {
	var missed *model.MissedAgent
	err := s.GetMaster().SelectOne(&missed, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", no_answers, member_stop_cause 
		from call_center.cc_attempt_leaving(:Id::int8, 'missed', :State, :AgentHoldTime, null::jsonb, :MaxAttempts::int, :WaitBetween::int) 
		as x (last_state_change timestamptz, no_answers int, member_stop_cause varchar)`,
		map[string]interface{}{
			"State":         model.ChannelStateMissed,
			"Id":            id,
			"AgentHoldTime": agentHoldTime,
			"MaxAttempts":   maxAttempts,
			"WaitBetween":   waitBetween,
		})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SetAttemptMissed", "store.sql_member.set_attempt_missed.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return missed, nil
}

func (s *SqlMemberStore) CancelAgentAttempt(id int64, agentHoldTime int) (*model.MissedAgent, *model.AppError) {
	var missed *model.MissedAgent
	err := s.GetMaster().SelectOne(&missed, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", no_answers
from call_center.cc_attempt_agent_cancel(:AttemptId::int8, :Result::varchar, :AgentState::varchar, :AgentHoldSec::int4)
    as x (last_state_change timestamptz, no_answers int)
where x.last_state_change notnull `,
		map[string]interface{}{
			"AttemptId":    id,
			"Result":       model.ChannelStateMissed,
			"AgentState":   model.ChannelStateMissed,
			"AgentHoldSec": agentHoldTime,
		})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.CancelAgentAttempt", "store.sql_member.set_attempt_agent_cancel.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return missed, nil
}

func (s *SqlMemberStore) SetBarred(id int64) *model.AppError {
	_, err := s.GetMaster().Exec(`with u as (
    update call_center.cc_member_attempt
        set leaving_at = now(),
            result = 'barred',
            state = 'leaving'
    where id = :AttemptId
    returning member_id, result
)
update call_center.cc_member m
set stop_at = now(),
    stop_cause = u.result
from u
where m.id = u.member_id`, map[string]interface{}{
		"AttemptId": id,
	})

	if err != nil {
		return model.NewAppError("SqlMemberStore.SetBarred", "store.sql_member.set_attempt_barred.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s *SqlMemberStore) SetAttemptResult(id int64, result string, channelState string, agentHoldTime int, vars map[string]string,
	maxAttempts uint, waitBetween uint64) (*model.MissedAgent, *model.AppError) {
	var missed *model.MissedAgent
	err := s.GetMaster().SelectOne(&missed, `select call_center.cc_view_timestamp(x.last_state_change)::int8 as "timestamp", no_answers,  member_stop_cause
		from call_center.cc_attempt_leaving(:Id::int8, :Result::varchar, :State, :AgentHoldTime, :Vars::jsonb, :MaxAttempts::int, :WaitBetween::int) 
		as x (last_state_change timestamptz, no_answers int, member_stop_cause varchar)`,
		map[string]interface{}{
			"Result":        result,
			"State":         channelState,
			"Id":            id,
			"AgentHoldTime": agentHoldTime,
			"Vars":          mapToJson(vars),
			"MaxAttempts":   maxAttempts,
			"WaitBetween":   waitBetween,
		})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SetAttemptResult", "store.sql_member.set_attempt_result.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return missed, nil
}

func (s *SqlMemberStore) GetTimeouts(nodeId string) ([]*model.AttemptReportingTimeout, *model.AppError) {
	var attempts []*model.AttemptReportingTimeout
	_, err := s.GetMaster().Select(&attempts, `select
       a.id attempt_id,
       call_center.cc_view_timestamp(call_center.cc_attempt_timeout(a.id, 0, 'abandoned', 'waiting', 0)) as timestamp,
       a.agent_id,
       ag.updated_at agent_updated_at,
       ag.user_id,
       ag.domain_id,
       a.channel
from call_center.cc_member_attempt a
    inner join call_center.cc_agent ag on ag.id = a.agent_id
    left join call_center.cc_queue cq on a.queue_id = cq.id
    left join call_center.cc_team ct on cq.team_id = ct.id
where a.timeout < now() and a.node_id = :NodeId`, map[string]interface{}{
		"NodeId": nodeId,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.GetTimeouts", "store.sql_member.get_timeouts.app_error", nil,
			err.Error(), http.StatusInternalServerError)
	}

	return attempts, nil
}

func (s *SqlMemberStore) CallbackReporting(attemptId int64, callback *model.AttemptCallback, maxAttempts uint, waitBetween uint64) (*model.AttemptReportingResult, *model.AppError) {
	var result *model.AttemptReportingResult
	err := s.GetMaster().SelectOne(&result, `select *
from call_center.cc_attempt_end_reporting(:AttemptId::int8, :Status::varchar, :Description::varchar, :ExpireAt::timestamptz, 
	:NextCallAt::timestamptz, :StickyAgentId::int, null::jsonb, :MaxAttempts::int, :WaitBetween::int, :ExcludeDest::bool) as
x (timestamp int8, channel varchar, queue_id int, agent_call_id varchar, agent_id int, user_id int8, domain_id int8, agent_timeout int8, member_stop_cause varchar)
where x.channel notnull`, map[string]interface{}{
		"AttemptId":     attemptId,
		"Status":        callback.Status,
		"Description":   callback.Description,
		"ExpireAt":      callback.ExpireAt,
		"NextCallAt":    callback.NextCallAt,
		"StickyAgentId": callback.StickyAgentId,
		"MaxAttempts":   maxAttempts,
		"WaitBetween":   waitBetween,
		"ExcludeDest":   callback.ExcludeCurrentCommunication,
	})

	if err != nil {
		code := extractCodeFromErr(err)
		if code == http.StatusNotFound {
			return nil, model.NewAppError("SqlMemberStore.Reporting", "store.sql_member.reporting.not_found", nil,
				err.Error(), code)
		} else {
			return nil, model.NewAppError("SqlMemberStore.Reporting", "store.sql_member.reporting.app_error", nil,
				err.Error(), code)
		}

	}

	return result, nil
}

func (s SqlMemberStore) SaveToHistory() ([]*model.HistoryAttempt, *model.AppError) {
	var res []*model.HistoryAttempt

	_, err := s.GetMaster().Select(&res, `with del as materialized (
    select *
    from call_center.cc_member_attempt a
    where a.state = 'leaving'
    for update skip locked
    limit 100
),
dd as (
    delete
    from call_center.cc_member_attempt m
    where m.id in (
        select del.id
        from del
    )
)
insert
into call_center.cc_member_attempt_history (id, domain_id, queue_id, member_id, weight, resource_id, result,
                                agent_id, bucket_id, destination, display, description, list_communication_id,
                                joined_at, leaving_at, agent_call_id, member_call_id, offering_at, reporting_at,
                                bridged_at, channel, seq, resource_group_id, answered_at, team_id,
								transferred_at, transferred_agent_id, transferred_attempt_id, parent_id, node_id)
select a.id, a.domain_id, a.queue_id, a.member_id, a.weight, a.resource_id, a.result, a.agent_id, a.bucket_id, a.destination,
       a.display, a.description, a.list_communication_id, a.joined_at, a.leaving_at, a.agent_call_id, a.member_call_id,
       a.offering_at, a.reporting_at, a.bridged_at, a.channel, a.seq, a.resource_group_id, a.answered_at, a.team_id,
	   a.transferred_at, a.transferred_agent_id, a.transferred_attempt_id, a.parent_id, a.node_id
from del a
returning id, result`)

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.SaveToHistory", "store.sql_member.save_history.app_error", nil,
			err.Error(), http.StatusInternalServerError)
	}
	return res, nil
}

func (s SqlMemberStore) CreateConversationChannel(parentChannelId, name string, attemptId int64) (string, *model.AppError) {
	res, err := s.GetMaster().SelectStr(`insert into call_center.cc_msg_participants (name, conversation_id, attempt_id)
select :Name, parent.conversation_id, :AttemptId
from call_center.cc_msg_participants parent
    inner join call_center.cc_msg_conversation cmc on parent.conversation_id = cmc.id
where parent.channel_id = :Parent and cmc.closed_at is null
returning channel_id`, map[string]interface{}{
		"Name":      name,
		"AttemptId": attemptId,
		"Parent":    parentChannelId,
	})

	if err != nil {
		return "", model.NewAppError("SqlMemberStore.CreateConversationChannel", "store.sql_member.create_conv_channel.app_error", nil,
			err.Error(), http.StatusInternalServerError)
	}

	return res, nil

}

func (s SqlMemberStore) RefreshQueueStatsLast2H() *model.AppError {
	_, err := s.GetMaster().Exec(`refresh materialized view call_center.cc_distribute_stats`)

	if err != nil {
		return model.NewAppError("SqlAgentStore.RefreshAgentPauseCauses", "store.sql_agent.refresh_pause_cause.app_error", nil,
			err.Error(), http.StatusInternalServerError)
	}

	return nil
}

func (s *SqlMemberStore) TransferredTo(id, toId int64) *model.AppError {
	_, err := s.GetMaster().Exec(`select * from call_center.cc_attempt_transferred_to(:Id, :ToId)
			as x (last_state_change timestamptz)`, map[string]interface{}{
		"Id":   id,
		"ToId": toId,
	})
	if err != nil {
		return model.NewAppError("SqlMemberStore.TransferredTo", "store.sql_member.set_attempt_trans_to.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s *SqlMemberStore) TransferredFrom(id, toId int64, toAgentId int, toAgentSessId string) *model.AppError {
	_, err := s.GetMaster().Exec(`select * from call_center.cc_attempt_transferred_from(:Id::int8, :ToId::int8, :ToAgentId::int, :ToAgentSessId::varchar)
			as x (last_state_change timestamptz)`, map[string]interface{}{
		"Id":            id,
		"ToId":          toId,
		"ToAgentId":     toAgentId,
		"ToAgentSessId": toAgentSessId,
	})

	if err != nil {
		return model.NewAppError("SqlMemberStore.TransferredFrom", "store.sql_member.set_attempt_trans_from.app_error", nil,
			fmt.Sprintf("AttemptId=%v %s", id, err.Error()), http.StatusInternalServerError)
	}

	return nil
}

func (s *SqlMemberStore) CancelAgentDistribute(agentId int32) ([]int64, *model.AppError) {
	var res []int64
	_, err := s.GetMaster().Select(&res, `
		update call_center.cc_member_attempt att
		set result = 'cancel'
		from (
			select a.id
			from call_center.cc_member_attempt a
				inner join call_center.cc_queue q on q.id = a.queue_id
			where a.agent_id = :AgentId
			  and q.type != 5
			  and not exists(
					select 1
					from call_center.cc_member_attempt a2
					where a2.agent_id = :AgentId
					  and a2.agent_call_id notnull
						for update
				)
		) t
		where t.id = att.id
		returning att.id`, map[string]interface{}{
		"AgentId": agentId,
	})

	if err != nil {
		return nil, model.NewAppError("SqlMemberStore.CancelAgentDistribute", "store.sql_member.cancel_agent_distribute.app_error", nil,
			fmt.Sprintf("AgentId=%v %s", agentId, err.Error()), http.StatusInternalServerError)
	}

	return res, nil
}
