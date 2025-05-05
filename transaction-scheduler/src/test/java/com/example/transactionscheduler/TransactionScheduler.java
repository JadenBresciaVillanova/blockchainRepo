package com.example.transactionscheduler;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Service;
import org.springframework.web.client.RestTemplate;

import java.util.Random;

@Service
public class TransactionScheduler {

    private static final Logger logger = LoggerFactory.getLogger(TransactionScheduler.class);

    @Value("${blockchain.node.transaction-url}")
    private String blockchainNodeTransactionUrl;

    private final RestTemplate restTemplate = new RestTemplate();

    private final Random random = new Random();
    private final String[] parties = {"Alice", "Bob", "Charlie", "David", "Eve", "Miner"};


    @Scheduled(fixedDelay = 2000) // Try a direct integer value
    public void sendRandomTransaction() {
    logger.info("Attempting to send transaction (Scheduler is running)");
    // ... rest of the method


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