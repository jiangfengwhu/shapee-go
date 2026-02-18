package routes

import (
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/services"
	"keepy-go/util"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TicketRoutes(r gin.IRouter, cfg *config.Config) {
	r.POST("/ticket/generate", func(c *gin.Context) {
		var req struct {
			DeviceID string `json:"device_id"`
		}
		c.ShouldBindJSON(&req)

		if req.DeviceID != "" {
			existingTicket, err := db.FindTicketByDeviceID(c.Request.Context(), req.DeviceID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if existingTicket != nil {
				encryptedID, err := util.EncryptTicketID(existingTicket.ID.Hex())
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt ticket ID"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"ticket_id": encryptedID})
				return
			}
		}

		id, err := db.GenerateTicket(c.Request.Context(), req.DeviceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		encryptedID, err := util.EncryptTicketID(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt ticket ID"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ticket_id": encryptedID})
	})

	r.POST("/ticket/subscription", func(c *gin.Context) {
		var req struct {
			EncryptedTicketID string `json:"ticket_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID, err := util.DecryptTicketID(req.EncryptedTicketID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
			return
		}

		ticket, err := db.GetTicket(c.Request.Context(), ticketID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"active":     ticket.IsSubscriptionActive(),
			"product_id": ticket.SubscriptionProductID,
			"expiry":     ticket.SubscriptionExpiry,
		})
	})

	appleService := services.NewAppleIAPService(&services.AppleIAPConfig{
		BundleID: cfg.AppleIAP.BundleID,
		Products: cfg.AppleIAP.Products,
	})

	r.POST("/ticket/apple-subscribe", func(c *gin.Context) {
		var req struct {
			EncryptedTicketID string `json:"ticket_id" binding:"required"`
			Receipt           string `json:"receipt" binding:"required"`
			ProductID         string `json:"product_id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID, err := util.DecryptTicketID(req.EncryptedTicketID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID"})
			return
		}

		ticketOID, err := bson.ObjectIDFromHex(ticketID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticket ID format"})
			return
		}

		result, err := appleService.VerifySignedTransaction(req.Receipt, req.ProductID)
		if err != nil {
			log.Printf("[Apple Subscribe] Verification failed: product_id=%s, error=%v, ip=%s", req.ProductID, err, c.ClientIP())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if result.ExpiresDate == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Not a subscription transaction"})
			return
		}

		expiresAt := time.UnixMilli(result.ExpiresDate)

		exists, existingTx, err := db.CheckAppleTransactionExists(c.Request.Context(), result.TransactionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check transaction"})
			return
		}

		if exists && existingTx.Status == db.AppleIAPStatusSuccess {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "Transaction already processed",
				"ticket_id": existingTx.TicketID.Hex(),
			})
			return
		}

		if exists && (existingTx.Status == db.AppleIAPStatusPending || existingTx.Status == db.AppleIAPStatusFailed) {
			if err := db.UpdateSubscription(c.Request.Context(), ticketOID, result.ProductID, expiresAt, result.OriginalTransactionID); err != nil {
				db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusFailed, err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusSuccess, "")

			log.Printf("[Apple Subscribe] Recovered: tx_id=%s, product_id=%s, expires=%s, ticket=%s",
				result.TransactionID, result.ProductID, expiresAt.Format(time.RFC3339), ticketID)

			c.JSON(http.StatusOK, gin.H{
				"apple_transaction_id": result.TransactionID,
				"product_id":           result.ProductID,
				"expires":              expiresAt,
				"recovered":            true,
			})
			return
		}

		input := &db.AppleIAPTransactionInput{
			TransactionID:         result.TransactionID,
			OriginalTransactionID: result.OriginalTransactionID,
			TicketID:              ticketOID,
			ProductID:             result.ProductID,
			Environment:           result.Environment,
			BundleID:              result.BundleID,
			AppAccountToken:       result.AppAccountToken,
			PurchaseDate:          result.PurchaseDate,
			ExpiresDate:           result.ExpiresDate,
			SignedDate:            result.SignedDate,
			TransactionType:       result.TransactionType,
			Quantity:              result.Quantity,
			Storefront:            result.Storefront,
			StorefrontID:          result.StorefrontID,
			Price:                 result.Price,
			Currency:              result.Currency,
		}

		if _, err := db.CreateAppleTransaction(c.Request.Context(), input); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record transaction: " + err.Error()})
			return
		}

		if err := db.UpdateSubscription(c.Request.Context(), ticketOID, result.ProductID, expiresAt, result.OriginalTransactionID); err != nil {
			db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusFailed, err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusSuccess, "")

		log.Printf("[Apple Subscribe] Success: tx_id=%s, product_id=%s, expires=%s, env=%s, ticket=%s",
			result.TransactionID, result.ProductID, expiresAt.Format(time.RFC3339), result.Environment, ticketID)

		c.JSON(http.StatusOK, gin.H{
			"apple_transaction_id": result.TransactionID,
			"product_id":           result.ProductID,
			"expires":              expiresAt,
			"environment":          result.Environment,
		})
	})

	notificationService := services.NewAppleNotificationService(cfg.AppleIAP.BundleID)

	r.POST("/webhook/apple", func(c *gin.Context) {
		var req struct {
			SignedPayload string `json:"signedPayload" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("[Apple Webhook] Invalid request body: %v, ip=%s", err, c.ClientIP())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		notification, err := notificationService.VerifyAndDecodeNotification(req.SignedPayload)
		if err != nil {
			log.Printf("[Apple Webhook] Notification verification failed: %v, ip=%s", err, c.ClientIP())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[Apple Webhook] Received: type=%s, subtype=%s, uuid=%s, env=%s",
			notification.NotificationType, notification.Subtype, notification.NotificationUUID, notification.Data.Environment)

		txInfo, err := notificationService.DecodeSignedTransactionInfo(notification.Data.SignedTransactionInfo)
		if err != nil {
			log.Printf("[Apple Webhook] Failed to decode transaction info: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[Apple Webhook] Transaction: tx_id=%s, orig_tx_id=%s, product_id=%s, expires=%d",
			txInfo.TransactionID, txInfo.OriginalTransactionID, txInfo.ProductID, txInfo.ExpiresDate)

		switch notification.NotificationType {
		case services.NotificationTypeDidRenew:
			ticket, err := db.FindTicketByOriginalTransactionID(c.Request.Context(), txInfo.OriginalTransactionID)
			if err != nil || ticket == nil {
				log.Printf("[Apple Webhook] Ticket not found for renewal: orig_tx_id=%s", txInfo.OriginalTransactionID)
				c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "note": "ticket not found"})
				return
			}

			expiresAt := time.UnixMilli(txInfo.ExpiresDate)
			if err := db.UpdateSubscription(c.Request.Context(), ticket.ID, txInfo.ProductID, expiresAt, txInfo.OriginalTransactionID); err != nil {
				log.Printf("[Apple Webhook] Failed to renew subscription: %v", err)
			}

			log.Printf("[Apple Webhook] Subscription renewed: ticket=%s, expires=%s", ticket.ID.Hex(), expiresAt.Format(time.RFC3339))
			c.JSON(http.StatusOK, gin.H{"status": "renewed"})

		case services.NotificationTypeExpired:
			ticket, err := db.FindTicketByOriginalTransactionID(c.Request.Context(), txInfo.OriginalTransactionID)
			if err != nil || ticket == nil {
				log.Printf("[Apple Webhook] Ticket not found for expiry: orig_tx_id=%s", txInfo.OriginalTransactionID)
				c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "note": "ticket not found"})
				return
			}

			log.Printf("[Apple Webhook] Subscription expired: ticket=%s", ticket.ID.Hex())
			c.JSON(http.StatusOK, gin.H{"status": "expired"})

		case services.NotificationTypeRefund, services.NotificationTypeRevoke:
			ticket, err := db.FindTicketByOriginalTransactionID(c.Request.Context(), txInfo.OriginalTransactionID)
			if err != nil || ticket == nil {
				log.Printf("[Apple Webhook] Ticket not found for refund/revoke: orig_tx_id=%s", txInfo.OriginalTransactionID)
				c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "note": "ticket not found"})
				return
			}

			if err := db.ExpireSubscription(c.Request.Context(), ticket.ID); err != nil {
				log.Printf("[Apple Webhook] Failed to expire subscription: %v", err)
			}

			exists, _, _ := db.CheckAppleTransactionExists(c.Request.Context(), txInfo.TransactionID)
			if exists {
				db.MarkTransactionRefunded(c.Request.Context(), txInfo.TransactionID)
			}

			log.Printf("[Apple Webhook] Subscription revoked/refunded: ticket=%s", ticket.ID.Hex())
			c.JSON(http.StatusOK, gin.H{"status": "revoked"})

		case services.NotificationTypeDidChangeRenewal:
			log.Printf("[Apple Webhook] Renewal status changed: orig_tx_id=%s, subtype=%s",
				txInfo.OriginalTransactionID, notification.Subtype)
			c.JSON(http.StatusOK, gin.H{"status": "acknowledged"})

		default:
			c.JSON(http.StatusOK, gin.H{"status": "acknowledged"})
		}
	})
}
