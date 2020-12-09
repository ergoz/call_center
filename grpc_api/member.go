package grpc_api

import (
	"context"
	"errors"
	"github.com/webitel/call_center/app"
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/queue"
	"github.com/webitel/protos/cc"
)

type member struct {
	app *app.App
}

func NewMemberApi(app *app.App) *member {
	return &member{app}
}

func (api *member) AttemptResult(ctx context.Context, in *cc.AttemptResultRequest) (*cc.AttemptResultResponse, error) {
	result := model.AttemptResult2{
		Success:     false,
		Status:      in.GetStatus(),
		Description: in.GetDescription(),
		Display:     in.GetDisplay(),
	}

	if in.ExpireAt > 0 {
		result.ExpireAt = model.NewInt64(in.GetExpireAt())
	}

	if in.NextDistributeAt > 0 {
		result.NextCall = model.NewInt64(in.NextDistributeAt)
	}

	err := api.app.Queue().Manager().ReportingAttempt(in.AttemptId, result)
	if err != nil {
		return nil, err
	}
	return &cc.AttemptResultResponse{
		Status: "success", //TODO
	}, nil
}

func (api *member) CallJoinToQueue(in *cc.CallJoinToQueueRequest, out cc.MemberService_CallJoinToQueueServer) error {

	ctx := context.Background()
	attempt, err := api.app.Queue().Manager().DistributeCall(ctx, in)
	if err != nil {
		return err
	}

	bridged := attempt.On(queue.AttemptHookBridgedAgent)
	leaving := attempt.On(queue.AttemptHookLeaving)

	for {
		select {
		case <-leaving:
			out.Send(&cc.QueueEvent{
				Data: &cc.QueueEvent_Leaving{
					Leaving: &cc.QueueEvent_LeavingData{
						Result: attempt.Result(),
					},
				},
			})
			goto stop
		case _, ok := <-bridged:
			if ok {
				out.Send(&cc.QueueEvent{
					Data: &cc.QueueEvent_Bridged{
						Bridged: &cc.QueueEvent_BridgedData{
							AgentId:     0, //TODO
							AgentCallId: "",
						},
					},
				})
			}
		}
	}

stop:

	return nil
}

func (api *member) ChatJoinToQueue(in *cc.ChatJoinToQueueRequest, out cc.MemberService_ChatJoinToQueueServer) error {
	return errors.New("TODO")
}

func (api *member) DirectAgentToMember(ctx context.Context, in *cc.DirectAgentToMemberRequest) (*cc.DirectAgentToMemberResponse, error) {
	res, err := api.app.Queue().Manager().DistributeDirectMember(in.GetMemberId(), int(in.GetCommunicationId()), int(in.GetAgentId()))
	if err != nil {
		return nil, err
	}

	return &cc.DirectAgentToMemberResponse{
		AttemptId: res.Id(),
	}, nil
}

func (api *member) EmailJoinToQueue(ctx context.Context, in *cc.EmailJoinToQueueRequest) (*cc.EmailJoinToQueueResponse, error) {
	return nil, nil
}
