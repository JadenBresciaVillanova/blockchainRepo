package com.example.transactionscheduler;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestTemplate;

import jakarta.annotation.PostConstruct; // Use jakarta for Spring Boot 3+

import java.util.Random;

@Service
public class TransactionScheduler {

    private static final Logger logger = LoggerFactory.getLogger(TransactionScheduler.class);

    @Value("${blockchain.node.transaction-url}")
    private String blockchainNodeTransactionUrl;

    // Note: The hardcoded fixedDelay=2000 in @Scheduled will be used
    // unless you change it to @Scheduled(fixedDelayString = "${scheduler.fixed-delay-ms}")
    // However, this doesn't explain why it's not running AT ALL.

    private final RestTemplate restTemplate = new RestTemplate(); // Consider letting Spring manage this if complex config needed later

    private final Random random = new Random();
    private final String[] parties = {"Alice", "Bob", "Charlie", "David", "Eve", "Miner"};

    // Add this method to check property injection
    @PostConstruct
    public void init() {
        logger.info("TransactionScheduler bean initialized.");
        logger.info("Configured blockchain.node.transaction-url: {}", blockchainNodeTransactionUrl);
        if (blockchainNodeTransactionUrl == null || blockchainNodeTransactionUrl.isEmpty() || !blockchainNodeTransactionUrl.equals("http://localhost:8081/transaction")) {
             logger.error("CRITICAL ERROR: blockchain.node.transaction-url property was NOT injected correctly or is unexpected!");
        }
    }


    @Scheduled(fixedDelay = 2000) // Using hardcoded value for now
    public void sendRandomTransaction() {
        // Add a log right at the start of the method
        logger.info("Attempting to send transaction (Scheduler method invoked)");

        // Check if URL is valid before attempting to send
        if (blockchainNodeTransactionUrl == null || blockchainNodeTransactionUrl.isEmpty()) {
             logger.error("blockchain.node.transaction-url is not set. Skipping transaction.");
             return;
        }

        // ... rest of your existing sendRandomTransaction method
         // Generate a random transaction
        String sender = parties[random.nextInt(parties.length)];
        String recipient = parties[random.nextInt(parties.length)];

        while (sender.equals(recipient) && parties.length > 1) {
             recipient = parties[random.nextInt(parties.length)];
        }

        int amount = random.nextInt(100) + 1;

        TransactionPayload payload = new TransactionPayload(sender, recipient, amount);

        try {
            logger.info("Attempting to send transaction to URL: {} Payload: {}", blockchainNodeTransactionUrl, payload);
            String response = restTemplate.postForObject(
                blockchainNodeTransactionUrl,
                payload,
                String.class
            );
            logger.info("Successfully sent transaction. Response: {}", response);

        } catch (Exception e) {
            logger.error("Failed to send transaction {}: {}", payload, e.getMessage(), e);
        }
    }
}