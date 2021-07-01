package sqlstore

import (
	"fmt"
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/store"
	"net/http"
)

type SqlCallStore struct {
	SqlStore
}

func NewSqlCallStore(sqlStore SqlStore) store.CallStore {
	as := &SqlCallStore{sqlStore}
	return as
}

func (s SqlCallStore) Get(id string) (*model.Call, *model.AppError) {
	var call *model.Call
	err := s.GetMaster().SelectOne(&call, `select id,
       direction,
       destination,
       parent_id,
       cc_view_timestamp(timestamp) as timestamp,
       app_id,
       from_number,
       from_name,
       domain_id,
       cc_view_timestamp(answered_at) as answered_at,
       cc_view_timestamp(bridged_at) as bridged_at,
       cc_view_timestamp(created_at) as created_at
from cc_calls c
where c.hangup_at isnull and c.Id = :Id
for update`, map[string]interface{}{
		"Id": id,
	})
	if err != nil {
		return nil, model.NewAppError("SqlAgentStore.Get", "store.sql_call.get.app_error", nil,
			fmt.Sprintf("Id=%v, %s", id, err.Error()), http.StatusInternalServerError)
	}

	return call, nil
}
