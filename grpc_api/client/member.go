package client

import (
	"context"
	proto "github.com/webitel/protos/cc"
)

type memberApi struct {
	cli *ccManager
}

func NewMemberApi(m *ccManager) MemberApi {
	return &memberApi{
		cli: m,
	}
}

func (api *memberApi) JoinCallToQueue(ctx context.Context, in *proto.CallJoinToQueueRequest) (proto.MemberService_CallJoinToQueueClient, error) {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return nil, err
	}

	return cli.member.CallJoinToQueue(ctx, in)
}

func (api *memberApi) JoinChatToQueue(ctx context.Context, in *proto.ChatJoinToQueueRequest) (proto.MemberService_ChatJoinToQueueClient, error) {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return nil, err
	}

	return cli.member.ChatJoinToQueue(ctx, in)
}

func (api *memberApi) DirectAgentToMember(domainId int64, memberId int64, communicationId int, agentId int64) (int64, error) {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return 0, err
	}
	res, err := cli.member.DirectAgentToMember(context.Background(), &proto.DirectAgentToMemberRequest{
		MemberId:        memberId,
		AgentId:         agentId,
		DomainId:        domainId,
		CommunicationId: int32(communicationId),
	})

	if err != nil {
		return 0, err
	}

	return res.AttemptId, nil
}

func (api *memberApi) AttemptResult(attemptId int64, status, description string, nextOffering *int64, expireAt *int64, vars map[string]string,
	stickyDisplay bool, agentId int32) error {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return err
	}

	result := &proto.AttemptResultRequest{
		AttemptId:   attemptId,
		Status:      status,
		Variables:   vars,
		Display:     stickyDisplay,
		Description: description,
		AgentId:     agentId,
	}

	if nextOffering != nil {
		result.NextDistributeAt = *nextOffering
	}

	if expireAt != nil {
		result.ExpireAt = *expireAt
	}

	_, err = cli.member.AttemptResult(context.Background(), result)

	if err != nil {
		return err
	}

	return nil
}

func (api *memberApi) RenewalResult(domainId, attemptId int64, renewal uint32) error {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return err
	}

	_, err = cli.member.AttemptRenewalResult(context.Background(), &proto.AttemptRenewalResultRequest{
		DomainId:  domainId,
		AttemptId: attemptId,
		Renewal:   renewal,
	})

	return err
}

func (api *memberApi) CallJoinToAgent(ctx context.Context, in *proto.CallJoinToAgentRequest) (proto.MemberService_CallJoinToAgentClient, error) {
	cli, err := api.cli.getRandomClient()
	if err != nil {
		return nil, err
	}

	return cli.member.CallJoinToAgent(ctx, in)
}
