package stream

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	TestMsgAUpsertKey = "testmsgA"
	TestMsgADeleteKey = "testmsgA_deleted"
	TestMsgBUpsertKey = "testmsgB"
	TestMsgBDeleteKey = "testmsgB_deleted"
	MsgDeleteType     = "delete"
	MsgUpdateType     = "update"
	MsgInsertType     = "insert"
)

type TestMsgTypeA struct {
	Seq int64
	ID  int
}

func (tm *TestMsgTypeA) GetID() int {
	return tm.ID
}

func (tm *TestMsgTypeA) SeqNum() int64 {
	return tm.Seq
}

func (tm *TestMsgTypeA) UpsertMsg() *UpsertMsg {
	return &UpsertMsg{
		JSONKey: TestMsgAUpsertKey,
		Msg:     tm,
	}
}

func (tm *TestMsgTypeA) DeleteMsg() *DeleteMsg {
	deleted := strconv.Itoa(tm.ID)
	return &DeleteMsg{
		Key:     TestMsgADeleteKey,
		Deleted: deleted,
	}
}

// A second test message type to help test that msgs are being distinguished from each other.
type TestMsgTypeB struct {
	Seq int64
	ID  int
}

func (tm *TestMsgTypeB) GetID() int {
	return tm.ID
}

func (tm *TestMsgTypeB) SeqNum() int64 {
	return tm.Seq
}

func (tm *TestMsgTypeB) UpsertMsg() *UpsertMsg {
	return &UpsertMsg{
		JSONKey: TestMsgBUpsertKey,
		Msg:     tm,
	}
}

func (tm *TestMsgTypeB) DeleteMsg() *DeleteMsg {
	deleted := strconv.Itoa(tm.ID)
	return &DeleteMsg{
		Key:     TestMsgBDeleteKey,
		Deleted: deleted,
	}
}

type TestEvent struct {
	Type          string
	FallinUserID  []int
	FalloutUserID []int
	BeforeSeq     int64
	AfterSeq      int64
}

func alwaysTrue[T Msg](msg T) bool {
	return true
}

func trueAfterN[T Msg](n int) func(T) bool {
	var msgCount int
	return func(T) bool {
		msgCount++
		return msgCount > n
	}
}

func msgFilter[T Msg](t *testing.T, fallinSeq int64, falloutSeq int64) func(T) bool {
	return func(msg T) bool {
		msgSeq := msg.SeqNum()
		switch {
		case fallinSeq < falloutSeq:
			switch {
			case msgSeq < fallinSeq:
				return false
			case fallinSeq <= msgSeq && msgSeq < falloutSeq:
				return true
			default:
				return false
			}
		case falloutSeq < fallinSeq:
			switch {
			case msgSeq < falloutSeq:
				return true
			case falloutSeq <= msgSeq && msgSeq < fallinSeq:
				return false
			default:
				return true
			}
		default:
			t.Errorf("falloutSeq can't equal fallinSeq.")
			return false
		}
	}
}

func falseBySeq[T Msg](seqs []int64) func(T) bool {
	return func(msg T) bool {
		return !slices.Contains(seqs, msg.SeqNum())
	}
}

func alwaysFalse[T Msg](msg T) bool {
	return false
}

type TestSubscriber struct {
	ID       int
	Streamer *Streamer
}

func prepareNothing(message MarshallableMsg) interface{} {
	return message
}

func TestConfigureSubscription(t *testing.T) {
	dummyFilter := func(msg *TestMsgTypeA) bool {
		return true
	}
	dummyHydrator := func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
		return &TestMsgTypeA{}, nil
	}
	streamer := NewStreamer(prepareNothing)
	publisher := NewPublisher[*TestMsgTypeA](dummyHydrator)
	sub := NewSubscription[*TestMsgTypeA](streamer, publisher, alwaysTrue[*TestMsgTypeA], dummyFilter)
	require.NotNil(t, sub.filter, "subscription filter is nil after instantiation")
	require.Empty(t, publisher.Subscriptions,
		"publisher's subscriptions are non-nil after instantiation")

	sub.Register()
	require.NotNil(t, sub.filter, "subscription filter is nil after configuration")
	require.True(t, sub.filter(&TestMsgTypeA{}), "set filter does not work")
	require.Len(t, publisher.Subscriptions, 1,
		"publisher's subscriptions are nil after configuration")
	for sub := range publisher.Subscriptions {
		require.True(t, sub.filter(&TestMsgTypeA{}),
			"publisher's subscription has the wrong filter")
	}

	sub2 := NewSubscription[*TestMsgTypeA](streamer, publisher, alwaysTrue[*TestMsgTypeA], alwaysFalse[*TestMsgTypeA])
	require.NotNil(t, sub2.filter, "subscription filter is nil after instantiation")

	sub2.Register()
	require.NotNil(t, sub2.filter, "subscription filter is nil after configuration")
	require.False(t, sub2.filter(&TestMsgTypeA{}), "set filter does not work")
	require.Len(t, publisher.Subscriptions, 2,
		"publisher's subscriptions are nil after configuration")

	sub.Unregister()
	require.Len(t, publisher.Subscriptions, 1,
		"publisher's still has subscriptions after deletion")
}

func TestBroadcast(t *testing.T) {
	hydrator := func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
		return &TestMsgTypeA{
			Seq: int64(msg.GetID()),
			ID:  msg.GetID(),
		}, nil
	}
	streamer := NewStreamer(prepareNothing)
	publisher := NewPublisher[*TestMsgTypeA](hydrator)
	trueSub := NewSubscription[*TestMsgTypeA](streamer, publisher, alwaysTrue[*TestMsgTypeA], alwaysTrue[*TestMsgTypeA])
	falseSub := NewSubscription[*TestMsgTypeA](streamer, publisher, alwaysTrue[*TestMsgTypeA], alwaysFalse[*TestMsgTypeA])
	trueSub.Register()
	falseSub.Register()
	afterMsg := TestMsgTypeA{
		Seq: 0,
		ID:  0,
	}
	event := Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg := map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 1, "upsert message was not upserted")
	upsertMsg, ok := streamer.Msgs[0].(*UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Zero(t, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	beforeMsg := TestMsgTypeA{
		Seq: 1,
		ID:  1,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Len(t, streamer.Msgs, 2, "delete message was not upsert")
	deleteMsg, ok := streamer.Msgs[1].(*DeleteMsg)
	require.True(t, ok, "message was not a delete type")
	require.Equal(t, "1", deleteMsg.Deleted)
}

func TestBroadcastWithFilters(t *testing.T) {
	streamer := NewStreamer(prepareNothing)
	hydrator := func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
		return &TestMsgTypeA{
			Seq: int64(msg.GetID()),
			ID:  msg.GetID(),
		}, nil
	}
	publisher := NewPublisher[*TestMsgTypeA](hydrator)
	publisherTwo := NewPublisher[*TestMsgTypeA](hydrator)

	// oneSub's filter expects to return true after receiving trueAfterCount messages
	trueAfterCount := 2
	oneSub := NewSubscription[*TestMsgTypeA](
		streamer,
		publisherTwo,
		alwaysTrue[*TestMsgTypeA],
		trueAfterN[*TestMsgTypeA](trueAfterCount),
	)
	falseSub := NewSubscription[*TestMsgTypeA](
		streamer,
		publisher,
		alwaysTrue[*TestMsgTypeA],
		alwaysFalse[*TestMsgTypeA],
	)
	oneSub.Register()
	falseSub.Register()

	// Msgs sent on publisher should not be sent.
	afterMsg := TestMsgTypeA{
		Seq: 0,
		ID:  0,
	}
	event := Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg := map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Zero(t, streamer.Msgs, "picked up message we don't want")

	beforeMsg := TestMsgTypeA{
		Seq: 1,
		ID:  1,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Zero(t, streamer.Msgs, "picked up message we don't want")

	afterMsg = TestMsgTypeA{
		Seq: 20,
		ID:  20,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Zero(t, streamer.Msgs, "picked up message we don't want")

	// Msgs sent on publisherTwo should be conditionally sent.
	afterMsg = TestMsgTypeA{
		Seq: 1,
		ID:  1,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Zero(t, streamer.Msgs, "picked up message we don't want")

	beforeMsg = TestMsgTypeA{
		Seq: 2,
		ID:  2,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Zero(t, len(streamer.Msgs), "picked up message we don't want")

	afterMsg = TestMsgTypeA{
		Seq: 3,
		ID:  3,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 1, "upsert message was not upserted")
	upsertMsg, ok := streamer.Msgs[0].(*UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	beforeMsg = TestMsgTypeA{
		Seq: 4,
		ID:  4,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	deleteMsg, ok := streamer.Msgs[1].(*DeleteMsg)
	require.Len(t, streamer.Msgs, 2, "upsert message was not upserted")
	require.True(t, ok, "message was not a delete type")
	require.Equal(t, "4", deleteMsg.Deleted, "Deleted number incorrect")

	// Msgs on publisher should not be sent
	afterMsg = TestMsgTypeA{
		Seq: 30,
		ID:  30,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 2, "upsert message was not upserted")
}

func TestBroadcastWithPermissionFilters(t *testing.T) {
	streamer := NewStreamer(prepareNothing)
	hydrator := func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
		return &TestMsgTypeA{
			Seq: int64(msg.GetID()),
			ID:  msg.GetID(),
		}, nil
	}
	publisher := NewPublisher[*TestMsgTypeA](hydrator)
	publisherTwo := NewPublisher[*TestMsgTypeA](hydrator)
	// oneSub's permission filter will return true after receiving trueAfterCount messages
	trueAfterCount := 2
	oneSub := NewSubscription[*TestMsgTypeA](
		streamer,
		publisherTwo,
		trueAfterN[*TestMsgTypeA](trueAfterCount),
		alwaysTrue[*TestMsgTypeA],
	)
	falseSub := NewSubscription[*TestMsgTypeA](
		streamer,
		publisher,
		alwaysFalse[*TestMsgTypeA],
		alwaysTrue[*TestMsgTypeA],
	)
	oneSub.Register()
	falseSub.Register()

	// Msgs sent on publisherTwo should be conditionally sent.
	afterMsg := TestMsgTypeA{
		Seq: 1,
		ID:  1,
	}
	event := Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg := map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	beforeMsg := TestMsgTypeA{
		Seq: 2,
		ID:  2,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Zero(t, len(streamer.Msgs), "picked up message we don't want")

	afterMsg = TestMsgTypeA{
		Seq: 3,
		ID:  3,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 1, "upsert message was not upserted")
	upsertMsg, ok := streamer.Msgs[0].(*UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	beforeMsg = TestMsgTypeA{
		Seq: 4,
		ID:  4,
	}
	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 2, "upsert message was not upserted")
	deleteMsg, ok := streamer.Msgs[1].(*DeleteMsg)
	require.True(t, ok, "message was not a delete type")
	require.Equal(t, "4", deleteMsg.Deleted, "Deleted number incorrect")

	// Msgs on publisher should not be sent.
	afterMsg = TestMsgTypeA{
		Seq: 3,
		ID:  3,
	}
	event = Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 2, "upsert message was not upserted")
}

func TestBroadcastSeparateEvents(t *testing.T) {
	streamer := NewStreamer(prepareNothing)
	streamerTwo := NewStreamer(prepareNothing)
	hydratorA := func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
		return &TestMsgTypeA{
			Seq: int64(msg.GetID()),
			ID:  msg.GetID(),
		}, nil
	}
	hydratorB := func(msg *TestMsgTypeB) (*TestMsgTypeB, error) {
		return &TestMsgTypeB{
			Seq: int64(msg.GetID()),
			ID:  msg.GetID(),
		}, nil
	}
	publisher := NewPublisher[*TestMsgTypeA](hydratorA)
	publisherTwo := NewPublisher[*TestMsgTypeB](hydratorB)
	publisherThree := NewPublisher[*TestMsgTypeB](hydratorB)
	trueSub := NewSubscription[*TestMsgTypeA](streamer, publisher, alwaysTrue[*TestMsgTypeA], alwaysTrue[*TestMsgTypeA])
	separateSub := NewSubscription[*TestMsgTypeB](
		streamerTwo, publisherTwo, alwaysTrue[*TestMsgTypeB], alwaysTrue[*TestMsgTypeB])
	togetherSub := NewSubscription[*TestMsgTypeB](
		streamer, publisherThree, alwaysTrue[*TestMsgTypeB], alwaysTrue[*TestMsgTypeB])
	trueSub.Register()
	separateSub.Register()
	togetherSub.Register()

	// Msgs sent on publisher should be picked up.
	afterMsg := TestMsgTypeA{
		Seq: 0,
		ID:  0,
	}
	event := Event[*TestMsgTypeA]{After: &afterMsg}
	idToSaturatedMsg := map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 1, "picked up message we don't want")
	upsertMsg, ok := streamer.Msgs[0].(*UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Zero(t, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	beforeMsg := TestMsgTypeA{
		Seq: 1,
		ID:  1,
	}

	event = Event[*TestMsgTypeA]{Before: &beforeMsg}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisher.HydrateMsg(event.After, idToSaturatedMsg)
	publisher.Broadcast([]Event[*TestMsgTypeA]{event}, idToSaturatedMsg)
	require.Len(t, streamer.Msgs, 2, "picked up message we don't want")
	deleteMsg, ok := streamer.Msgs[1].(*DeleteMsg)
	require.True(t, ok, "message was not a delete type")
	require.Equal(t, "1", deleteMsg.Deleted, "Deleted number incorrect")

	// Msgs sent on publisherTwo should not be picked up.
	afterMsgB := TestMsgTypeB{
		Seq: 2,
		ID:  2,
	}
	eventB := Event[*TestMsgTypeB]{After: &afterMsgB}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisherTwo.HydrateMsg(eventB.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeB]{eventB}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 2, "picked up message we don't want")

	beforeMsgB := TestMsgTypeB{
		Seq: 3,
		ID:  3,
	}
	eventB = Event[*TestMsgTypeB]{Before: &beforeMsgB}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisherTwo.HydrateMsg(eventB.After, idToSaturatedMsg)
	publisherTwo.Broadcast([]Event[*TestMsgTypeB]{eventB}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 2, "picked up message we don't want")

	// Msgs sent onf publisherthree should be picked up.
	afterMsgB = TestMsgTypeB{
		Seq: 4,
		ID:  4,
	}
	eventB = Event[*TestMsgTypeB]{After: &afterMsgB}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisherThree.HydrateMsg(eventB.After, idToSaturatedMsg)
	publisherThree.Broadcast([]Event[*TestMsgTypeB]{eventB}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 3, "upsert message was not upserted")
	upsertMsg, ok = streamer.Msgs[2].(*UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 4, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	beforeMsgB = TestMsgTypeB{
		Seq: 5,
		ID:  5,
	}
	eventB = Event[*TestMsgTypeB]{Before: &beforeMsgB}
	idToSaturatedMsg = map[int]*UpsertMsg{}
	publisherThree.HydrateMsg(eventB.After, idToSaturatedMsg)
	publisherThree.Broadcast([]Event[*TestMsgTypeB]{eventB}, idToSaturatedMsg)

	require.Len(t, streamer.Msgs, 4, "upsert message was not upserted")
	deleteMsg, ok = streamer.Msgs[3].(*DeleteMsg)
	require.True(t, ok, "message was not a delete type")
	require.Equal(t, "5", deleteMsg.Deleted, "Deleted number incorrect")
}

func setup(t *testing.T, testEvents []TestEvent, testSubscribers []TestSubscriber) {
	var events []Event[*TestMsgTypeA]
	userToFalloutSeq := make(map[int]int64)
	userToFallinSeq := make(map[int]int64)

	for _, testEvent := range testEvents {
		var event Event[*TestMsgTypeA]
		switch testEvent.Type {
		case MsgInsertType:
			event = Event[*TestMsgTypeA]{
				After: &TestMsgTypeA{
					Seq: testEvent.AfterSeq,
					ID:  0,
				},
			}
		case MsgUpdateType:
			event = Event[*TestMsgTypeA]{
				Before: &TestMsgTypeA{
					Seq: testEvent.AfterSeq - 1,
					ID:  0,
				},
				After: &TestMsgTypeA{
					Seq: testEvent.AfterSeq,
					ID:  0,
				},
			}

			for _, userID := range testEvent.FalloutUserID {
				userToFalloutSeq[userID] = testEvent.AfterSeq
			}
			for _, userID := range testEvent.FallinUserID {
				userToFallinSeq[userID] = testEvent.AfterSeq
			}
		case MsgDeleteType:
			event = Event[*TestMsgTypeA]{Before: &TestMsgTypeA{
				Seq: testEvent.BeforeSeq,
				ID:  0,
			}}
		}
		events = append(events, event)
	}

	// Setting fallout seq for users do not have a fallout event. It's for creating subscription filter.
	for _, ts := range testSubscribers {
		if _, ok := userToFalloutSeq[ts.ID]; !ok {
			userToFalloutSeq[ts.ID] = int64(len(testEvents) + 1)
		}
	}
	// Setting fallin seq for users do not have a fallin event.
	for _, ts := range testSubscribers {
		if _, ok := userToFallinSeq[ts.ID]; !ok {
			userToFallinSeq[ts.ID] = int64(-1)
		}
	}

	var hydrator func(*TestMsgTypeA) (*TestMsgTypeA, error)
	if testEvents[len(testEvents)-1].Type != MsgDeleteType {
		lastSeq := testEvents[len(testEvents)-1].AfterSeq

		hydrator = func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
			return &TestMsgTypeA{
				Seq: lastSeq,
				ID:  msg.GetID(),
			}, nil
		}
	} else {
		hydrator = func(msg *TestMsgTypeA) (*TestMsgTypeA, error) {
			return nil, sql.ErrNoRows
		}
	}

	publisher := NewPublisher[*TestMsgTypeA](hydrator)
	for _, ts := range testSubscribers {
		subscriber := NewSubscription[*TestMsgTypeA](
			ts.Streamer,
			publisher,
			msgFilter[*TestMsgTypeA](t, userToFallinSeq[ts.ID], userToFalloutSeq[ts.ID]),
			alwaysTrue[*TestMsgTypeA],
		)
		subscriber.Register()
	}

	idToSaturatedMsg := map[int]*UpsertMsg{}
	for _, ev := range events {
		publisher.HydrateMsg(ev.After, idToSaturatedMsg)
	}

	publisher.Broadcast(events, idToSaturatedMsg)
}

// Up to four DB events are included in the TestTwoSubscribers. Update on id 0, subscriber1 fallout on id 0,
// subsriber2 fallout on id 0, delete id 0. Permutate these events to generate the tests in this function.
func TestTwoSubscribers(t *testing.T) {
	type testCase struct {
		description  string
		dBEvents     []TestEvent
		outGoingMsgs []interface{}
	}

	tcs := []testCase{
		{
			description: "1. insert id 0(1), update on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgUpdateType, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "2. insert id 0(1), subscriber1 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "3. insert id 0(1), subscriber2 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "4. insert id 0(1), subscriber1 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "5. insert id 0(1), subscriber2 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "6. insert id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgInsertType, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{},
		},
		{
			description: "7. update on id 0(1), subscriber1 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "8. update on id 0(1), subscriber2 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "9. update on id 0(1), subscriber1 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "10. update on id 0(1), subscriber2 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "11. update on id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "12. subscriber1 fallin on id 0(1), update on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "13. subscriber1 fallin on id 0(1), subscriber2 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "14. subscriber1 fallin on id 0(1), subscriber1 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "15. subscriber1 fallin on id 0(1), subscriber2 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "16. subscriber1 fallin on id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "17. subscriber2 fallin on id 0(1), update on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "18. subscriber2 fallin on id 0(1), subscriber1 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "19. subscriber2 fallin on id 0(1), subscriber1 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "20. subscriber2 fallin on id 0(1), subscriber2 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "21. subscriber2 fallin on id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "22. subscriber1 fallout on id 0(1), update on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "23. subscriber1 fallout on id 0(1), subscriber1 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "24. subscriber1 fallout on id 0(1), subscriber2 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "25. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "26. subscriber1 fallout on id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "27. subscriber2 fallout on id 0(1), update on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "28. subscriber2 fallout on id 0(1), subscriber1 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "29. subscriber2 fallout on id 0(1), subscriber2 fallin on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FallinUserID: []int{2}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
				DeleteMsg{Deleted: "0"},
				UpsertMsg{Msg: &TestMsgTypeA{2, 0}},
			},
		},
		{
			description: "30. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 1},
				{Type: MsgUpdateType, FalloutUserID: []int{1}, AfterSeq: 2},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				DeleteMsg{Deleted: "0"},
			},
		},
		{
			description: "31. subscriber2 fallout on id 0(1), delete id 0(2)",
			dBEvents: []TestEvent{
				{Type: MsgUpdateType, FalloutUserID: []int{2}, AfterSeq: 1},
				{Type: MsgDeleteType, BeforeSeq: 1},
			},
			outGoingMsgs: []interface{}{
				DeleteMsg{Deleted: "0"},
				DeleteMsg{Deleted: "0"},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			testSubscribers := []TestSubscriber{{1, NewStreamer(prepareNothing)}, {2, NewStreamer(prepareNothing)}}
			setup(
				t,
				tc.dBEvents,
				testSubscribers,
			)

			var streamerMsgs []interface{}
			for _, ts := range testSubscribers {
				streamerMsgs = append(streamerMsgs, ts.Streamer.Msgs...)
			}
			require.Equal(t, len(tc.outGoingMsgs), len(streamerMsgs), "streamer.Msgs length incorrect")

			for i, o := range tc.outGoingMsgs {
				switch ot := o.(type) {
				case UpsertMsg:
					upsertMsg, ok := streamerMsgs[i].(*UpsertMsg)
					require.True(t, ok, "message was not an upsert type")
					require.Equal(t, ot.Msg.SeqNum(), upsertMsg.Msg.SeqNum(), "Sequence number incorrect")
				case DeleteMsg:
					deleteMsg, ok := streamerMsgs[i].(*DeleteMsg)
					require.True(t, ok, "message was not an delete type")
					require.Equal(t, "0", deleteMsg.Deleted)
				}
			}
		})
	}
}

func helper(streamer *Streamer, testEvents []TestEvent, orderedUserIDs []int) {
	publisher := NewPublisher[TestMsgTypeA]()

	var events []Event[TestMsgTypeA]
	var seqs []int64
	userToFalloutSeqs := make(map[int][]int64)

	for _, testEvent := range testEvents {
		var event Event[TestMsgTypeA]
		switch testEvent.Type {
		case "update":
			event = Event[TestMsgTypeA]{After: &TestMsgTypeA{
				Seq: int64(testEvent.Seq),
				ID:  0,
			}}
		case "fallout":
			event = Event[TestMsgTypeA]{
				Before: &TestMsgTypeA{
					Seq: int64(testEvent.Seq - 1),
					ID:  0,
				},
				After: &TestMsgTypeA{
					Seq: int64(testEvent.Seq),
					ID:  0,
				}}
			userToFalloutSeqs[testEvent.UserID] = append(userToFalloutSeqs[testEvent.UserID], int64(testEvent.Seq))
		case "delete":
			event = Event[TestMsgTypeA]{Before: &TestMsgTypeA{
				Seq: int64(testEvent.Seq),
				ID:  0,
			}}
		}
		events = append(events, event)
	}
	fmt.Printf("userToFalloutSeqs: %+v\n", userToFalloutSeqs)

	entityDeleted := false
	for _, userID := range orderedUserIDs {
		fmt.Println("in orderUesrIDs loop")
		numOfEvents := len(testEvents)
		for range numOfEvents {
			fmt.Println("in testEvents loop")
			testEvent := testEvents[0]
			// if len(testEvents) > 1 {
			testEvents = testEvents[1:]
			// }

			switch testEvent.Type {
			case "update":
				seqs = append(seqs, testEvent.Seq)

			case "fallout":
				if userID != testEvent.UserID {
					seqs = append(seqs, testEvent.Seq)
				} else {
					testEvents = append(testEvents, testEvent)
				}

			case "delete":
				entityDeleted = true

			}
		}
		if entityDeleted {
			// break because entity deleted, doesn't need hydration anymore.
			break
		}
	}
	fmt.Printf("seqs: %+v\n", seqs)
	hydrator := func(ID int) (TestMsgTypeA, error) {
		seq := seqs[0]
		seqs = seqs[1:]
		return TestMsgTypeA{
			Seq: int64(seq),
			ID:  ID,
		}, nil
	}
	Subscriber1 := NewSubscription[TestMsgTypeA](streamer, publisher, falseBySeq[TestMsgTypeA](userToFalloutSeqs[1]), alwaysTrue[TestMsgTypeA], hydrator)
	Subscriber2 := NewSubscription[TestMsgTypeA](streamer, publisher, falseBySeq[TestMsgTypeA](userToFalloutSeqs[2]), alwaysTrue[TestMsgTypeA], hydrator)
	Subscriber1.Register()
	Subscriber2.Register()

	publisher.Broadcast(events)
}

// target_seq: 4
// 1. update on id 0(1), subscriber1 fallout on id 0(2), subscriber2 fallout on id 0(3), delete id 0(4)
// 2. update on id 0(1), subscriber2 fallout on id 0(2), subscriber1 fallout on id 0(3), delete id 0(4)
// 3. subscriber1 fallout on id 0(1), update on id 0(2), subscriber2 fallout on id 0(3), delete id 0(4)
// 4. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2), update on id 0(3), delete id 0(4)
// 5. subscriber2 fallout on id 0(1), update on id 0(2), subscriber1 fallout on id 0(3), delete id 0(4)
// 6. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2), update on id 0(3), delete id 0(4)
// target_seq: 3
// 7. update on id 0(1), subscriber1 fallout on id 0(2), subscriber2 fallout on id 0(3)
// 8. update on id 0(1), subscriber1 fallout on id 0(2), delete id 0(3)
// 9. update on id 0(1), subscriber2 fallout on id 0(2), subscriber1 fallout on id 0(3)
// 10. update on id 0(1), subscriber2 fallout on id 0(2), delete id 0(3)
// 11. subscriber1 fallout on id 0(1), update on id 0(2), subscriber2 fallout on id 0(3)
// 12. subscriber1 fallout on id 0(1), update on id 0(2), delete id 0(3)
// 13. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2), update on id 0(3)
// 14. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2), delete id 0(3)
// 15. subscriber2 fallout on id 0(1), update on id 0(2), subscriber1 fallout on id 0(3)
// 16. subscriber2 fallout on id 0(1), update on id 0(2), delete id 0(3)
// 17. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2), update on id 0(3)
// 18. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2), delete id 0(3)
// target_seq: 2
// 19. update on id 0(1), subscriber1 fallout on id 0(2)
// 20. update on id 0(1), subscriber2 fallout on id 0(2)
// 21. update on id 0(1), delete id 0(2)
// 22. subscriber1 fallout on id 0(1), update on id 0(2)
// 23. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2)
// 24. subscriber1 fallout on id 0(1), delete id 0(2)
// 25. subscriber2 fallout on id 0(1), update on id 0(2)
// 26. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2)
// 27. subscriber2 fallout on id 0(1), delete id 0(2)
// target_seq: 1
// 28. update on id 0(1)
// 29. subscriber1 fallout on id 0(1)
// 30. subscriber2 fallout on id 0(1)
// 31. delete id 0
func TestTwoSubscribers(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// update on id 0(1), subscriber1 fallout on id 0(2), subscriber2 fallout on id 0(3), delete id 0(4). It has duplicate delete events.
	helper(
		streamer,
		[]TestEvent{
			{Type: "update", Seq: 1},
			{Type: "fallout", UserID: 1, Seq: 2},
			{Type: "fallout", UserID: 2, Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	upsertMsg, ok := streamer.Msgs[0].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 1, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok := streamer.Msgs[1].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	upsertMsg, ok = streamer.Msgs[2].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}

func TestTwoSubscribers2(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// 2. update on id 0(1), subscriber2 fallout on id 0(2), subscriber1 fallout on id 0(3), delete id 0(4)
	helper(
		streamer,
		[]TestEvent{
			{Type: "update", Seq: 1},
			{Type: "fallout", UserID: 2, Seq: 2},
			{Type: "fallout", UserID: 1, Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	upsertMsg, ok := streamer.Msgs[0].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 1, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	upsertMsg, ok = streamer.Msgs[1].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 2, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok := streamer.Msgs[2].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}

func TestTwoSubscribers3(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// 3. subscriber1 fallout on id 0(1), update on id 0(2), subscriber2 fallout on id 0(3), delete id 0(4)
	helper(
		streamer,
		[]TestEvent{
			{Type: "fallout", UserID: 1, Seq: 1},
			{Type: "update", Seq: 2},
			{Type: "fallout", UserID: 2, Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	deleteMsg, ok := streamer.Msgs[0].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	upsertMsg, ok := streamer.Msgs[1].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 2, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	upsertMsg, ok = streamer.Msgs[2].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}

func TestTwoSubscribers4(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// 4. subscriber1 fallout on id 0(1), subscriber2 fallout on id 0(2), update on id 0(3), delete id 0(4)
	helper(
		streamer,
		[]TestEvent{
			{Type: "fallout", UserID: 1, Seq: 1},
			{Type: "fallout", UserID: 2, Seq: 2},
			{Type: "update", Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	deleteMsg, ok := streamer.Msgs[0].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	upsertMsg, ok := streamer.Msgs[1].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 2, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	upsertMsg, ok = streamer.Msgs[2].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}

func TestTwoSubscribers5(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// 5. subscriber2 fallout on id 0(1), update on id 0(2), subscriber1 fallout on id 0(3), delete id 0(4)
	helper(
		streamer,
		[]TestEvent{
			{Type: "fallout", UserID: 2, Seq: 1},
			{Type: "update", Seq: 2},
			{Type: "fallout", UserID: 1, Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	upsertMsg, ok := streamer.Msgs[0].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 1, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	upsertMsg, ok = streamer.Msgs[1].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 2, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok := streamer.Msgs[2].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}

func TestTwoSubscribers6(t *testing.T) {
	streamer := NewStreamer(prepareNothing)

	// 6. subscriber2 fallout on id 0(1), subscriber1 fallout on id 0(2), update on id 0(3), delete id 0(4)
	helper(
		streamer,
		[]TestEvent{
			{Type: "fallout", UserID: 2, Seq: 1},
			{Type: "fallout", UserID: 1, Seq: 2},
			{Type: "update", Seq: 3},
			{Type: "delete"}},
		[]int{1, 2},
	)

	require.Equal(t, 5, len(streamer.Msgs), "streamer.Msgs length incorrect")

	upsertMsg, ok := streamer.Msgs[0].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 1, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok := streamer.Msgs[1].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	upsertMsg, ok = streamer.Msgs[2].(UpsertMsg)
	require.True(t, ok, "message was not an upsert type")
	require.Equal(t, 3, int(upsertMsg.Msg.SeqNum()), "Sequence number incorrect")

	deleteMsg, ok = streamer.Msgs[3].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)

	deleteMsg, ok = streamer.Msgs[4].(DeleteMsg)
	require.True(t, ok, "message was not an delete type")
	require.Equal(t, "0", deleteMsg.Deleted)
}
