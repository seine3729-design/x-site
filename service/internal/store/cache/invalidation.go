package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Invalidator 通过 Valkey Pub/Sub 广播缓存失效（docs/03 第 9.1、9.2 节，S1-9）。
//
// Valkey 是所有实例共享的二级缓存，删一次即全局生效；一级缓存（进程内）各实例独立，
// 删不到别的实例的本地副本。本总线把「某类配置已变更」广播给所有实例，让每个实例清掉自己的本地缓存，
// 做到「改配置后各实例秒级生效」。消息体是 topic 字符串（如 "site_config"），不带负载：
// 订阅方据此判断清哪一类缓存，具体清什么由领域层决定。
type Invalidator struct {
	client  valkey.Client
	channel string
}

// NewInvalidator 创建失效总线，channel 是所有实例约定的 Pub/Sub 频道名。
func NewInvalidator(client valkey.Client, channel string) *Invalidator {
	return &Invalidator{client: client, channel: channel}
}

// Publish 广播一条失效消息。发布失败时各实例靠一级缓存的短 TTL 兜底，所以调用方记日志即可、不必中断本次写入。
func (v *Invalidator) Publish(ctx context.Context, topic string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return v.client.Do(ctx, v.client.B().Publish().Channel(v.channel).Message(topic).Build()).Error()
}

// subscribeBackoff 是订阅连接断开后重连前的等待。
const subscribeBackoff = time.Second

// Subscribe 在后台订阅频道，每条消息把 topic 交给 onTopic；返回的 stop 停止订阅并等待后台 goroutine 退出。
// 连接断开时自动重连（重连期间漏掉的消息由一级缓存的短 TTL 兜底）。onTopic 在订阅 goroutine 中串行调用，
// 不要在其中做耗时阻塞的事。
func (v *Invalidator) Subscribe(onTopic func(topic string)) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sub := v.client.B().Subscribe().Channel(v.channel).Build()
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			// Receive 阻塞直到订阅被取消（ctx done）、连接关闭（ErrClosing）或出错返回。
			err := v.client.Receive(ctx, sub, func(msg valkey.PubSubMessage) { onTopic(msg.Message) })
			if ctx.Err() != nil || errors.Is(err, valkey.ErrClosing) {
				return
			}
			slog.Warn("缓存失效订阅中断，重连中", "channel", v.channel, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(subscribeBackoff):
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
