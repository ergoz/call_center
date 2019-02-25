package sqlstore

import (
	"github.com/webitel/call_center/model"
	"github.com/webitel/call_center/store"
	"net/http"
)

type SqlQueueStore struct {
	SqlStore
}

func NewSqlQueueStore(sqlStore SqlStore) store.QueueStore {
	us := &SqlQueueStore{sqlStore}
	return us
}

func (self *SqlQueueStore) CreateIndexesIfNotExists() {

}

func (s SqlQueueStore) ReserveMembersByNode(nodeId string) store.StoreChannel {
	return store.Do(func(result *store.StoreResult) {
		if i, err := s.GetMaster().SelectInt(`select s as count
			from reserve_members_with_resources($1) s`, nodeId); err != nil {
			result.Err = model.NewAppError("SqlQueueStore.ReserveMembers", "store.sql_queue.reserve_member_resources.app_error",
				map[string]interface{}{"Error": err.Error()},
				err.Error(), http.StatusInternalServerError)
		} else {
			result.Data = i
		}
	})
}

func (s SqlQueueStore) UnReserveMembersByNode(nodeId, cause string) store.StoreChannel {
	return store.Do(func(result *store.StoreResult) {
		if i, err := s.GetMaster().SelectInt(`select s as count
			from un_reserve_members_with_resources($1, $2) s`, nodeId, cause); err != nil {
			result.Err = model.NewAppError("SqlQueueStore.UnReserveMembers", "store.sql_queue.un_reserve_member_resources.app_error",
				map[string]interface{}{"Error": err.Error()},
				err.Error(), http.StatusInternalServerError)
		} else {
			result.Data = i
		}
	})
}
