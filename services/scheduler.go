package services

import (
	"context"
	"log"
	"shapee-go/config"
	"shapee-go/db"
	"time"
)

type Scheduler struct {
	cfg  *config.Config
	apns *APNsService
	stop chan struct{}
}

func NewScheduler(cfg *config.Config, apns *APNsService) *Scheduler {
	return &Scheduler{
		cfg:  cfg,
		apns: apns,
		stop: make(chan struct{}),
	}
}

func (s *Scheduler) Start() {
	log.Println("[Scheduler] 启动后台调度器")
	go s.runPushDeliveryLoop()
	go s.runReminderLoop()
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

// runPushDeliveryLoop 每分钟检查到期的推送任务并发送
func (s *Scheduler) runPushDeliveryLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.deliverDuePushTasks()
		}
	}
}

func (s *Scheduler) deliverDuePushTasks() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tasks, err := db.GetDuePushTasks(ctx, 100)
	if err != nil {
		log.Printf("[Scheduler] 获取推送任务失败: %v", err)
		return
	}

	for _, task := range tasks {
		ticket, err := db.GetTicket(ctx, task.TicketID.Hex())
		if err != nil || ticket.DeviceToken == "" {
			db.MarkPushTaskFailed(ctx, task.ID, "用户无推送token")
			continue
		}

		if s.apns.IsConfigured() {
			if err := s.apns.SendPush(ticket.DeviceToken, task.Title, task.Body); err != nil {
				log.Printf("[Scheduler] 推送失败 ticket=%s: %v", task.TicketID.Hex(), err)
				db.MarkPushTaskFailed(ctx, task.ID, err.Error())
				continue
			}
		} else {
			log.Printf("[Scheduler] APNs未配置，模拟推送: title=%s body=%s", task.Title, task.Body)
		}

		db.MarkPushTaskSent(ctx, task.ID)
		log.Printf("[Scheduler] 推送成功 ticket=%s type=%s title=%s", task.TicketID.Hex(), task.Type, task.Title)
	}
}

// runReminderLoop 每分钟检查所有用户，按各自设定时间创建体重提醒
func (s *Scheduler) runReminderLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.checkAndCreateReminders()
		}
	}
}

const followUpDelayHours = 3

func (s *Scheduler) checkAndCreateReminders() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tickets, err := db.GetAllTicketsWithDeviceToken(ctx)
	if err != nil {
		log.Printf("[Scheduler] 获取用户列表失败: %v", err)
		return
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	nowH, nowM := now.Hour(), now.Minute()

	for _, t := range tickets {
		rh := t.GetReminderHour()
		rm := t.GetReminderMinute()

		reminderCount, _ := db.CountWeightRemindersToday(ctx, t.ID)

		// 首次提醒：到了用户设定的提醒时间，且今天还没有发过提醒
		if reminderCount == 0 && (nowH > rh || (nowH == rh && nowM >= rm)) {
			scheduledAt := time.Date(now.Year(), now.Month(), now.Day(), rh, rm, 0, 0, loc).UTC()
			task := &db.PushTask{
				TicketID:    t.ID,
				Type:        db.PushTaskWeightReminder,
				Title:       "早安！请更新今日体重 🌅",
				Body:        "记录体重是减肥成功的第一步，快来更新今天的体重吧！",
				ScheduledAt: scheduledAt,
			}
			if err := db.CreatePushTask(ctx, task); err != nil {
				log.Printf("[Scheduler] 创建体重提醒失败 ticket=%s: %v", t.ID.Hex(), err)
			}
		}

		// 催促提醒：提醒时间 + 3小时后，用户仍未更新体重，且只发过一次提醒
		followUpH := rh + followUpDelayHours
		if reminderCount == 1 && (nowH > followUpH || (nowH == followUpH && nowM >= rm)) {
			hasWeight, _ := db.HasWeightRecordToday(ctx, t.ID.Hex())
			if hasWeight {
				continue
			}

			followUpAt := time.Date(now.Year(), now.Month(), now.Day(), followUpH, rm, 0, 0, loc).UTC()
			task := &db.PushTask{
				TicketID:    t.ID,
				Type:        db.PushTaskWeightReminder,
				Title:       "别忘了记录体重哦！⚖️",
				Body:        "今天还没有更新体重，更新后将为你生成专属的饮食和锻炼计划！",
				ScheduledAt: followUpAt,
			}
			if err := db.CreatePushTask(ctx, task); err != nil {
				log.Printf("[Scheduler] 创建催促提醒失败 ticket=%s: %v", t.ID.Hex(), err)
			}
		}
	}
}
