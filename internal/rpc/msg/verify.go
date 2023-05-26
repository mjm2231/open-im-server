package msg

import (
	"context"
	"math/rand"
	"strconv"
	"time"

	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/config"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/common/constant"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/errs"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/proto/msg"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/proto/sdkws"
	"github.com/OpenIMSDK/Open-IM-Server/pkg/utils"
)

var (
	ExcludeContentType = []int{constant.HasReadReceipt, constant.GroupHasReadReceipt}
)

type Validator interface {
	validate(pb *msg.SendMsgReq) (bool, int32, string)
}

type MessageRevoked struct {
	RevokerID                   string `json:"revokerID"`
	RevokerRole                 int32  `json:"revokerRole"`
	ClientMsgID                 string `json:"clientMsgID"`
	RevokerNickname             string `json:"revokerNickname"`
	RevokeTime                  int64  `json:"revokeTime"`
	SourceMessageSendTime       int64  `json:"sourceMessageSendTime"`
	SourceMessageSendID         string `json:"sourceMessageSendID"`
	SourceMessageSenderNickname string `json:"sourceMessageSenderNickname"`
	SessionType                 int32  `json:"sessionType"`
	Seq                         uint32 `json:"seq"`
}

func (m *msgServer) userIsMuteAndIsAdminInGroup(ctx context.Context, groupID, userID string) (isMute bool, err error) {
	groupMemberInfo, err := m.Group.GetGroupMemberInfo(ctx, groupID, userID)
	if err != nil {
		return false, err
	}
	if groupMemberInfo.MuteEndTime >= time.Now().Unix() {
		return true, nil
	}
	return false, nil
}

// 如果禁言了，再看下是否群管理员
func (m *msgServer) groupIsMuted(ctx context.Context, groupID string, userID string) (bool, bool, error) {
	groupInfo, err := m.Group.GetGroupInfo(ctx, groupID)
	if err != nil {
		return false, false, err
	}

	if groupInfo.Status == constant.GroupStatusMuted {
		groupMemberInfo, err := m.Group.GetGroupMemberInfo(ctx, groupID, userID)
		if err != nil {
			return false, false, err
		}
		return true, groupMemberInfo.RoleLevel > constant.GroupOrdinaryUsers, nil
	}
	return false, false, nil
}

func (m *msgServer) GetGroupMemberIDs(ctx context.Context, groupID string) (groupMemberIDs []string, err error) {
	return m.GroupLocalCache.GetGroupMemberIDs(ctx, groupID)
}

func (m *msgServer) messageVerification(ctx context.Context, data *msg.SendMsgReq) ([]string, error) {
	switch data.MsgData.SessionType {
	case constant.SingleChatType:
		if utils.IsContain(data.MsgData.SendID, config.Config.Manager.AppManagerUid) {
			return nil, nil
		}
		if data.MsgData.ContentType <= constant.NotificationEnd && data.MsgData.ContentType >= constant.NotificationBegin {
			return nil, nil
		}
		black, err := m.black.IsBlocked(ctx, data.MsgData.SendID, data.MsgData.RecvID)
		if err != nil {
			return nil, err
		}
		if black {
			return nil, errs.ErrBlockedByPeer.Wrap()
		}
		if *config.Config.MessageVerify.FriendVerify {
			friend, err := m.friend.IsFriend(ctx, data.MsgData.SendID, data.MsgData.RecvID)
			if err != nil {
				return nil, err
			}
			if !friend {
				return nil, errs.ErrNotPeersFriend.Wrap()
			}
			return nil, nil
		}
		return nil, nil
	case constant.SuperGroupChatType:
		groupInfo, err := m.Group.GetGroupInfo(ctx, data.MsgData.GroupID)
		if err != nil {
			return nil, err
		}
		if groupInfo.GroupType == constant.SuperGroup {
			return nil, nil
		}
		userIDList, err := m.GetGroupMemberIDs(ctx, data.MsgData.GroupID)
		if err != nil {
			return nil, err
		}
		if utils.IsContain(data.MsgData.SendID, config.Config.Manager.AppManagerUid) {
			return nil, nil
		}
		if data.MsgData.ContentType <= constant.NotificationEnd && data.MsgData.ContentType >= constant.NotificationBegin {
			return userIDList, nil
		} else {
			if !utils.IsContain(data.MsgData.SendID, userIDList) {
				return nil, errs.ErrNotInGroupYet.Wrap()
			}
		}
		isMute, err := m.userIsMuteAndIsAdminInGroup(ctx, data.MsgData.GroupID, data.MsgData.SendID)
		if err != nil {
			return nil, err
		}
		if isMute {
			return nil, errs.ErrMutedInGroup.Wrap()
		}

		isMute, isAdmin, err := m.groupIsMuted(ctx, data.MsgData.GroupID, data.MsgData.SendID)
		if err != nil {
			return nil, err
		}
		if isAdmin {
			return userIDList, nil
		}
		if isMute {
			return nil, errs.ErrMutedGroup.Wrap()
		}
		return userIDList, nil

	default:
		return nil, nil
	}
}
func (m *msgServer) encapsulateMsgData(msg *sdkws.MsgData) {
	msg.ServerMsgID = GetMsgID(msg.SendID)
	msg.SendTime = utils.GetCurrentTimestampByMill()
	switch msg.ContentType {
	case constant.Text:
		fallthrough
	case constant.Picture:
		fallthrough
	case constant.Voice:
		fallthrough
	case constant.Video:
		fallthrough
	case constant.File:
		fallthrough
	case constant.AtText:
		fallthrough
	case constant.Merger:
		fallthrough
	case constant.Card:
		fallthrough
	case constant.Location:
		fallthrough
	case constant.Custom:
		fallthrough
	case constant.Quote:
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, true)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, true)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderSync, true)
	case constant.Revoke:
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	case constant.HasReadReceipt:
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	case constant.Typing:
		utils.SetSwitchFromOptions(msg.Options, constant.IsHistory, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsPersistent, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderSync, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsSenderConversationUpdate, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsUnreadCount, false)
		utils.SetSwitchFromOptions(msg.Options, constant.IsOfflinePush, false)
	}
}

func GetMsgID(sendID string) string {
	t := time.Now().Format("2006-01-02 15:04:05")
	return utils.Md5(t + "-" + sendID + "-" + strconv.Itoa(rand.Int()))
}

func (m *msgServer) modifyMessageByUserMessageReceiveOpt(ctx context.Context, userID, conversationID string, sessionType int, pb *msg.SendMsgReq) (bool, error) {
	opt, err := m.User.GetUserGlobalMsgRecvOpt(ctx, userID)
	if err != nil {
		return false, err
	}
	switch opt {
	case constant.ReceiveMessage:
	case constant.NotReceiveMessage:
		return false, nil
	case constant.ReceiveNotNotifyMessage:
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		utils.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	// conversationID := utils.GetConversationIDBySessionType(conversationID, sessionType)
	singleOpt, err := m.Conversation.GetSingleConversationRecvMsgOpt(ctx, userID, conversationID)
	if errs.ErrRecordNotFound.Is(err) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	switch singleOpt {
	case constant.ReceiveMessage:
		return true, nil
	case constant.NotReceiveMessage:
		if utils.IsContainInt(int(pb.MsgData.ContentType), ExcludeContentType) {
			return true, nil
		}
		return false, nil
	case constant.ReceiveNotNotifyMessage:
		if pb.MsgData.Options == nil {
			pb.MsgData.Options = make(map[string]bool, 10)
		}
		utils.SetSwitchFromOptions(pb.MsgData.Options, constant.IsOfflinePush, false)
		return true, nil
	}
	return true, nil
}