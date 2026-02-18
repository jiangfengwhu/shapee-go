package routes

import (
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/services"
	"keepy-go/util"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type RechargeRequest struct {
	EncryptedTicketID string `json:"ticket_id" binding:"required"`
	Amount            int64  `json:"amount" binding:"required"`
}

type AppleRechargeRequest struct {
	EncryptedTicketID string `json:"ticket_id" binding:"required"`
	Receipt           string `json:"receipt" binding:"required"`
	ProductID         string `json:"product_id" binding:"required"`
}

func TicketRoutes(r gin.IRouter, cfg *config.Config) {
	r.POST("/ticket/generate", func(c *gin.Context) {
		var req struct {
			DeviceID string `json:"device_id"`
		}
		// Ignore bind errors since device_id is optional
		c.ShouldBindJSON(&req)

		// If device_id is provided, check for existing ticket
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

	r.POST("/ticket/balance", func(c *gin.Context) {
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
		c.JSON(http.StatusOK, ticket)
	})

	// Apple IAP recharge endpoint (StoreKit 2)
	appleService := services.NewAppleIAPService(&services.AppleIAPConfig{
		BundleID: cfg.AppleIAP.BundleID,
		Products: cfg.AppleIAP.Products,
	})

	r.POST("/ticket/apple-recharge", func(c *gin.Context) {
		var req AppleRechargeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Decrypt ticket ID
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

		// Verify signed transaction (StoreKit 2 JWS format)
		result, err := appleService.VerifySignedTransaction(req.Receipt, req.ProductID)
		if err != nil {
			log.Printf("[Apple IAP] Verification failed: product_id=%s, error=%v, ip=%s", req.ProductID, err, c.ClientIP())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Check if transaction already processed
		exists, existingTx, err := db.CheckAppleTransactionExists(c.Request.Context(), result.TransactionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check transaction"})
			return
		}

		if exists {
			// If previous transaction was successful, return duplicate error
			if existingTx.Status == db.AppleIAPStatusSuccess {
				c.JSON(http.StatusConflict, gin.H{
					"error":     "Transaction already processed",
					"ticket_id": existingTx.TicketID.Hex(),
				})
				return
			}

			// If previous transaction was pending or failed, try to process it again
			// This handles the edge case where Apple payment succeeded but our DB update failed
			if existingTx.Status == db.AppleIAPStatusPending || existingTx.Status == db.AppleIAPStatusFailed {
				// Attempt recharge
				if rechargeErr := db.RechargeTicket(c.Request.Context(), ticketID, result.Amount); rechargeErr != nil {
					db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusFailed, rechargeErr.Error(), bson.ObjectID{})
					c.JSON(http.StatusInternalServerError, gin.H{"error": rechargeErr.Error()})
					return
				}

				// Update status to success
				db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusSuccess, "", ticketOID)

				log.Printf("[Apple IAP] Recovered transaction: tx_id=%s, product_id=%s, amount=%d, env=%s, ticket=%s",
					result.TransactionID, result.ProductID, result.Amount, result.Environment, ticketID)

				c.JSON(http.StatusOK, gin.H{
					"apple_transaction_id": result.TransactionID,
					"product_id":           result.ProductID,
					"amount":               result.Amount,
					"environment":          result.Environment,
					"recovered":            true,
				})
				return
			}
		}

		// STEP 1: Record transaction as PENDING first (before attempting recharge)
		// This ensures we have a record even if the recharge fails
		input := &db.AppleIAPTransactionInput{
			TransactionID:         result.TransactionID,
			OriginalTransactionID: result.OriginalTransactionID,
			TicketID:              ticketOID,
			ProductID:             result.ProductID,
			Amount:                result.Amount,
			Environment:           result.Environment,
			BundleID:              result.BundleID,
			AppAccountToken:       result.AppAccountToken,
			PurchaseDate:          result.PurchaseDate,
			SignedDate:            result.SignedDate,
			TransactionType:       result.TransactionType,
			Quantity:              result.Quantity,
			Storefront:            result.Storefront,
			StorefrontID:          result.StorefrontID,
			Price:                 result.Price,
			Currency:              result.Currency,
		}

		_, err = db.CreateAppleTransaction(c.Request.Context(), input)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record transaction: " + err.Error()})
			return
		}

		// STEP 2: Attempt to recharge the ticket
		if rechargeErr := db.RechargeTicket(c.Request.Context(), ticketID, result.Amount); rechargeErr != nil {
			// Update transaction status to FAILED
			db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusFailed, rechargeErr.Error(), bson.ObjectID{})
			c.JSON(http.StatusInternalServerError, gin.H{"error": rechargeErr.Error()})
			return
		}

		// STEP 3: Update transaction status to SUCCESS
		db.UpdateAppleTransactionStatus(c.Request.Context(), result.TransactionID, db.AppleIAPStatusSuccess, "", ticketOID)

		log.Printf("[Apple IAP] Success: tx_id=%s, product_id=%s, amount=%d, env=%s, ticket=%s, storefront=%s",
			result.TransactionID, result.ProductID, result.Amount, result.Environment, ticketID, result.Storefront)

		c.JSON(http.StatusOK, gin.H{
			"apple_transaction_id": result.TransactionID,
			"product_id":           result.ProductID,
			"amount":               result.Amount,
			"environment":          result.Environment,
		})
	})

	// Apple Server Notification V2 webhook for refunds
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

		// Verify and decode the notification
		notification, err := notificationService.VerifyAndDecodeNotification(req.SignedPayload)
		if err != nil {
			log.Printf("[Apple Webhook] Notification verification failed: %v, ip=%s", err, c.ClientIP())
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		log.Printf("[Apple Webhook] Received: type=%s, subtype=%s, uuid=%s, env=%s",
			notification.NotificationType, notification.Subtype, notification.NotificationUUID, notification.Data.Environment)

		// Handle REFUND notifications
		if notification.NotificationType == services.NotificationTypeRefund {
			// Decode the transaction info
			txInfo, err := notificationService.DecodeSignedTransactionInfo(notification.Data.SignedTransactionInfo)
			if err != nil {
				log.Printf("[Apple Webhook] Failed to decode transaction info: %v", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			log.Printf("[Apple Webhook] Refund transaction: tx_id=%s, product_id=%s, env=%s",
				txInfo.TransactionID, txInfo.ProductID, txInfo.Environment)

			// Find the original transaction in our database
			exists, originalTx, err := db.CheckAppleTransactionExists(c.Request.Context(), txInfo.TransactionID)
			if err != nil {
				log.Printf("[Apple Webhook] Failed to check transaction: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check transaction"})
				return
			}

			if !exists {
				// Transaction not found - might be old or from different system
				log.Printf("[Apple Webhook] Refund for unknown transaction: tx_id=%s", txInfo.TransactionID)
				// Still return 200 to acknowledge receipt to Apple
				c.JSON(http.StatusOK, gin.H{"status": "acknowledged", "note": "transaction not found"})
				return
			}

			// Check if already refunded
			if originalTx.Status == db.AppleIAPStatusRefunded {
				log.Printf("[Apple Webhook] Transaction already refunded: tx_id=%s", txInfo.TransactionID)
				c.JSON(http.StatusOK, gin.H{"status": "already processed"})
				return
			}

			// Deduct balance from ticket
			err = db.DeductTicketBalance(c.Request.Context(), originalTx.TicketID, originalTx.Amount)
			if err != nil {
				log.Printf("[Apple Webhook] Failed to deduct balance: tx_id=%s, error=%v", txInfo.TransactionID, err)
				// Still try to mark as refunded
			}

			// Mark transaction as refunded
			if err := db.MarkTransactionRefunded(c.Request.Context(), txInfo.TransactionID); err != nil {
				log.Printf("[Apple Webhook] Failed to mark refunded: tx_id=%s, error=%v", txInfo.TransactionID, err)
			}

			log.Printf("[Apple Webhook] Refund processed: tx_id=%s, ticket=%s, amount=%d",
				txInfo.TransactionID, originalTx.TicketID.Hex(), originalTx.Amount)

			c.JSON(http.StatusOK, gin.H{"status": "refund processed"})
			return
		}

		// For other notification types, just acknowledge
		c.JSON(http.StatusOK, gin.H{"status": "acknowledged"})
	})
}
