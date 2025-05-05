package main

import (
	"context" // Import the context package
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math" // Needed for chunk calculation
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"runtime" // Import runtime to get number of CPU cores
	"sort" // For sorting transactions deterministically
	"strconv"
	"strings"
	"time"
)

// Difficulty - Number of leading zeros required for a valid hash
const difficulty = 5 // Increased difficulty slightly for potential longer mining

// Transaction represents a single transaction in a block
type Transaction struct {
	ID string `json:"id"` // Unique ID for the transaction (e.g., hash)
	Sender string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount int `json:"amount"`
	Timestamp string `json:"timestamp"` // Timestamp of the transaction
}

// Block represents a block in the blockchain
type Block struct {
	Index     int `json:"index"`
	Timestamp string `json:"timestamp"`
	Transactions []Transaction `json:"transactions"` // Added Transactions field
	Hash      string `json:"hash"`
	PrevHash  string `json:"prevHash"`
	Nonce     int `json:"nonce"` // Added Nonce field
}

// Function to generate a simple unique ID for a transaction (using hash)
func generateTransactionID(tx Transaction) string {
    record := tx.Sender + tx.Recipient + strconv.Itoa(tx.Amount) + tx.Timestamp
    h := sha256.New()
    h.Write([]byte(record))
    return hex.EncodeToString(h.Sum(nil))
}

// sortTransactions deterministically sorts transactions for consistent hashing
func sortTransactions(txs []Transaction) {
    sort.SliceStable(txs, func(i, j int) bool {
        // Sort by ID (hash), which is unique and deterministic
        return txs[i].ID < txs[j].ID
    })
}

// transactionsToString generates a deterministic string representation of transactions for hashing
func transactionsToString(txs []Transaction) string {
    // Create a copy to avoid modifying the original slice if it's used elsewhere unsorted
    txsCopy := make([]Transaction, len(txs))
    copy(txsCopy, txs)

    sortTransactions(txsCopy) // Sort deterministically

    var sb strings.Builder
    for _, tx := range txsCopy {
        // Concatenate transaction fields in a fixed order
        sb.WriteString(tx.ID)
        sb.WriteString(tx.Sender)
        sb.WriteString(tx.Recipient)
        sb.WriteString(strconv.Itoa(tx.Amount))
        // We don't include the tx timestamp in the hash string because the block timestamp is sufficient
        // and tx timestamps might vary slightly even for the same logical transaction set.
    }
    return sb.String()
}


// calculateHash generates the SHA256 hash for a block, including the nonce and transactions
func calculateHash(block Block) string {
	// Ensure transactions are included deterministically in the hash input
	transactionsStr := transactionsToString(block.Transactions)

	record := strconv.Itoa(block.Index) + block.Timestamp + transactionsStr + block.PrevHash + strconv.Itoa(block.Nonce)
	h := sha256.New()
	h.Write([]byte(record))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

// isHashValid checks if a hash meets the difficulty criteria (has enough leading zeros)
func isHashValid(hash string, difficulty int) bool {
	if len(hash) < difficulty {
		return false
	}
	prefix := strings.Repeat("0", difficulty)
	return strings.HasPrefix(hash, prefix)
}

// generateBlock creates a new block given the previous block and transaction data (used for manual mode)
// Note: This function does NOT perform mining/PoW. The Nonce will typically be 0.
// This function is now less needed as Mode 1 creates the block structure inline,
// but keeping it in case it's used elsewhere or for clarity.
func generateBlock(oldBlock Block, transactions []Transaction) Block {
    var newBlock Block
	newBlock.Index = oldBlock.Index + 1
	newBlock.Timestamp = time.Now().String()
	newBlock.Transactions = transactions
	newBlock.PrevHash = oldBlock.Hash
	newBlock.Nonce = 0 // Manual blocks don't perform mining, Nonce is typically 0 or default
	newBlock.Hash = calculateHash(newBlock) // Calculate hash with the default nonce
	return newBlock
}


// --- Mining Functions (Modified to handle transactions) ---

// worker finds a valid nonce within a given range for a block candidate
// It sends the block to the result channel if found, or stops if context is cancelled.
// Now accepts transactions.
func worker(ctx context.Context, startNonce, endNonce int, oldBlock Block, transactions []Transaction, difficulty int, resultChan chan<- Block) {
	candidateBlock := Block{
		Index:     oldBlock.Index + 1,
		Timestamp: time.Now().String(), // Capture timestamp when work starts
		Transactions: transactions,
		PrevHash:  oldBlock.Hash,
		Nonce:     startNonce, // Start searching from this nonce
	}

	// Sort transactions ONCE for deterministic hashing within the worker
	sortTransactions(candidateBlock.Transactions)


	for nonce := startNonce; nonce <= endNonce; nonce++ {
		// Check if context is cancelled (another worker found the solution)
		select {
		case <-ctx.Done():
			// fmt.Printf("Worker %d-%d: Context cancelled. Stopping.\n", startNonce, endNonce) // Optional: for debugging workers
			return // Stop this goroutine
		default:
			// Continue working
		}

		candidateBlock.Nonce = nonce // Update the nonce
		candidateHash := calculateHash(candidateBlock) // Calculate hash (uses the sorted transactions)

		if isHashValid(candidateHash, difficulty) {
			candidateBlock.Hash = candidateHash // Set the valid hash
			// fmt.Printf("Worker %d-%d: Found valid hash with Nonce %d!\n", startNonce, endNonce, nonce) // Optional: for debugging workers
			select {
			case resultChan <- candidateBlock: // Send the found block
				// Successfully sent the block
			case <-ctx.Done():
				// Context was cancelled while trying to send, just return
			}
			return // Stop this goroutine
		}

		// Optional: Add a tiny sleep here to make mining visible/slower, or prevent 100% CPU usage if difficulty is very low
		// time.Sleep(time.Nanosecond)
	}
	// fmt.Printf("Worker %d-%d: Finished range without finding a hash.\n", startNonce, endNonce) // Optional: for debugging workers
}


// mineBlock orchestrates parallel mining using multiple workers
// Now accepts transactions to include in the block.
func mineBlock(oldBlock Block, transactions []Transaction, difficulty int, numWorkers int) Block {
	fmt.Printf("Mining block %d with %d worker(s) (%d transactions)...\n", oldBlock.Index + 1, numWorkers, len(transactions))

	// Set a max search space (Adjust this based on difficulty and expected mining time)
	const maxNonceSearch = 2000000000 // Increased search space for potentially higher difficulty/more data

	resultChan := make(chan Block)
	ctx, cancel := context.WithCancel(context.Background()) // Context for cancellation

	// Divide the nonce search space among workers
	chunkSize := int(math.Ceil(float64(maxNonceSearch) / float64(numWorkers)))

	for i := 0; i < numWorkers; i++ {
		startNonce := i * chunkSize
		endNonce := startNonce + chunkSize - 1
		if endNonce >= maxNonceSearch {
            endNonce = maxNonceSearch - 1 // Ensure we don't go past the max limit
            if startNonce >= maxNonceSearch { // If start is already beyond limit, this worker does nothing
                 break
            }
        }
        if startNonce < 0 { startNonce = 0 } // Ensure start is not negative

		// Pass the transactions to the worker
		go worker(ctx, startNonce, endNonce, oldBlock, transactions, difficulty, resultChan)
	}

	// Wait for the first result
	minedBlock := <-resultChan

	// Cancel other workers once a result is received
	cancel()

	fmt.Printf("Block %d mined with hash %s (Nonce: %d)\n", minedBlock.Index, minedBlock.Hash, minedBlock.Nonce)

	// Give workers a moment to receive the cancel signal before returning
	time.Sleep(10 * time.Millisecond) // Small delay to allow graceful shutdown

	return minedBlock
}


// isBlockValid checks if a new block is valid by comparing it to the old block and the difficulty
// This is used for verifying blocks received (e.g., from other nodes, or our own mined blocks)
// Now verifies the hash calculation using the transactions in the block.
func isBlockValid(newBlock, oldBlock Block, difficulty int) bool {
	// Check if index is correct
	if oldBlock.Index+1 != newBlock.Index {
		log.Printf("Validation failed for Block %d: Index mismatch. Expected %d, got %d", newBlock.Index, oldBlock.Index+1, newBlock.Index)
		return false
	}
	// Check if previous hash matches
	if oldBlock.Hash != newBlock.PrevHash {
		log.Printf("Validation failed for Block %d: Previous hash mismatch. Expected %s, got %s", newBlock.Index, oldBlock.Hash, newBlock.PrevHash)
		return false
	}
    // Check if the new block's hash is correctly calculated *using its stored nonce and transactions*
    if calculateHash(newBlock) != newBlock.Hash {
		log.Printf("Validation failed for Block %d: Hash calculation mismatch. Recalculated %s (with Nonce %d, Transactions %v), stored %s", newBlock.Index, calculateHash(newBlock), newBlock.Nonce, newBlock.Transactions, newBlock.Hash)
        return false
    }
	// Check if the new block's hash meets the difficulty criteria
	// This check is crucial for mined blocks (Automatic Mode / Transactions Mode).
	// For Manual Mode blocks (Nonce=0), this will likely fail if difficulty > 0.
	if !isHashValid(newBlock.Hash, difficulty) {
		log.Printf("Validation failed for Block %d: Hash %s does not meet difficulty %d ('%s' prefix)", newBlock.Index, newBlock.Hash, difficulty, strings.Repeat("0", difficulty))
		return false
	}
	return true
}

var Blockchain []Block // Global variable to hold the blockchain
// Add package-level variables to store the selected mode and blocks to process
var selectedMode int
var totalBlocksToProcess int
var miningMode string 


// exportBlockchainToJSON saves the current blockchain to a JSON file
func exportBlockchainToJSON() {
	data, err := json.MarshalIndent(Blockchain, "", "  ")
	if err != nil {
		log.Printf("Error marshaling blockchain: %v", err)
        return
	}
	err = ioutil.WriteFile("blockchain.json", data, 0644)
	if err != nil {
		log.Printf("Error writing blockchain.json: %v", err)
        return
	}
	// fmt.Println("Blockchain data exported to blockchain.json") // Avoid spamming during mining
}

// openBrowser opens the specified URL in the default web browser
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // Linux and other Unix-like systems
		cmd = "xdg-open"
		args = []string{url}
	}

	err := exec.Command(cmd, args...).Start()
	if err != nil {
		log.Printf("Failed to open browser: %v. Please open %s manually.", err, url)
	}
}

// generateRandomTransactions creates a slice of random transactions
func generateRandomTransactions(count int) []Transaction {
    txs := make([]Transaction, count)
    // Simulate some random senders/recipients
    senders := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Miner"}
    recipients := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Miner"}

    for i := 0; i < count; i++ {
        sender := senders[rand.Intn(len(senders))]
        recipient := recipients[rand.Intn(len(recipients))]
        // Ensure sender and recipient are different unless it's a self-transfer (less common)
        for sender == recipient && len(senders) > 1 { // Avoid infinite loop if only one sender/recipient
             recipient = recipients[rand.Intn(len(recipients))]
        }


        amount := rand.Intn(100) + 1 // Random amount between 1 and 100

        tx := Transaction{
            Sender: sender,
            Recipient: recipient,
            Amount: amount,
            Timestamp: time.Now().Format(time.RFC3339Nano), // Use a consistent format
        }
        // Generate ID AFTER other fields are set to ensure consistency
        tx.ID = generateTransactionID(tx)
        txs[i] = tx
    }
    return txs
}

// --- HTTP Handlers ---

// rootHandler serves index.html for modes 1/2 and index2.html for mode 3
func rootHandler(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers first for all handlers that the browser might access
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

    // Handle OPTIONS preflight request
    if r.Method == "OPTIONS" {
        return
    }

	// Serve different HTML based on the globally selected mode
	if selectedMode == 3 {
		log.Println("Serving index2.html for Mode 3")
		http.ServeFile(w, r, "index2.html")
	} else {
		log.Println("Serving index.html for Mode", selectedMode)
		// Serve index.html for modes 1 and 2
		http.ServeFile(w, r, "index.html")
	}
}

// blockchainHandler serves the current state of the blockchain and total blocks to process
func blockchainHandler(w http.ResponseWriter, r *http.Request) {
	// CORS headers are set by rootHandler for simplicity, but good practice to set them here too if this handler might be called directly
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

    // Handle OPTIONS preflight request
    if r.Method == "OPTIONS" {
        return
    }

	w.Header().Set("Content-Type", "application/json")

	response := struct {
		Blockchain           []Block `json:"blockchain"`
		TotalBlocksToProcess int     `json:"totalBlocksToProcess"`
	}{
		Blockchain:           Blockchain,
		TotalBlocksToProcess: totalBlocksToProcess, // Use the package-level variable
	}

	json.NewEncoder(w).Encode(response)
}


func main() {
    rand.Seed(time.Now().UnixNano()) // Seed the random number generator

	// Create the genesis block (index 0) - typically not mined via PoW
    // Genesis block has no transactions and Nonce 0
	genesisBlock := Block{Index: 0, Timestamp: time.Now().String(), Transactions: []Transaction{}, Hash: "", PrevHash: "", Nonce: 0}
	genesisBlock.Hash = calculateHash(genesisBlock) // Calculate hash for genesis with its initial Nonce
	Blockchain = append(Blockchain, genesisBlock)
	fmt.Println("Genesis block created.")
    fmt.Printf("Genesis Hash: %s (Nonce: %d)\n", genesisBlock.Hash, genesisBlock.Nonce)

	// --- Mode Selection ---
	fmt.Println("\nChoose a mode:")
	fmt.Println("1. Manual Block Entry (Blocks added directly, dummy transaction, no PoW mining)")
	fmt.Println("2. Automatic Block Generation & Mining (Single dummy transaction)") // Keep for comparison/simplicity
	fmt.Println("3. Automatic Block Generation & Mining (With Random Transactions)") // New Mode
	fmt.Print("Enter mode number (1, 2, or 3): ")

    // Read and store the mode globally
	var inputMode int
	_, err := fmt.Scanln(&inputMode)
	if err != nil || (inputMode != 1 && inputMode != 2 && inputMode != 3) {
		log.Fatalf("Invalid input for mode selection. Please enter 1, 2, or 3.")
        os.Exit(1)
	}
    selectedMode = inputMode // Store the selected mode globally


    // Get number of blocks to process if in automatic modes
    if selectedMode == 2 || selectedMode == 3 {
		fmt.Print("Enter Number of Blocks to Process (mine): ")
        var inputNumBlocks int
		_, err = fmt.Scanln(&inputNumBlocks)
        if err != nil {
            log.Printf("Error reading number of blocks input: %v", err)
            os.Exit(1)
        }
        if inputNumBlocks <= 0 {
             log.Fatalf("Number of blocks must be positive.")
             os.Exit(1)
        }
        totalBlocksToProcess = inputNumBlocks // Store the number of blocks globally


        fmt.Println("\nChoose automatic mining mode:")
        fmt.Println("a. Single-threaded Mining (1 worker)")
        fmt.Println("b. Multi-threaded Mining (using CPU cores)")
        fmt.Print("Enter mining mode (a or b): ")
        _, err = fmt.Scanln(&miningMode) // miningMode is local to main, used only here
        miningMode = strings.ToLower(miningMode) // Make input case-insensitive
        if err != nil || (miningMode != "a" && miningMode != "b") {
             log.Fatalf("Invalid input for mining mode. Please enter 'a' or 'b'.")
             os.Exit(1)
        }
    }


	// --- Block Generation Logic based on Mode ---
	// Run generation/mining in a goroutine so the main thread can start the server
	go func() {
        switch selectedMode { // Use the globally selectedMode
        case 1:
            fmt.Println("\n--- Manual Block Entry Mode Selected ---")

            var money int // Local variable for money input in mode 1
            fmt.Print("Enter Money value (-1 to stop adding blocks): ")
            _, err := fmt.Scanln(&money)
            if err != nil {
                 log.Printf("Error reading Money input: %v", err)
            }

            for money != -1 {
                lastBlock := Blockchain[len(Blockchain)-1] // Get the last block

                // Create transactions slice for the new block
                 transactions := []Transaction{}
                 if money >= 0 { // Only add a transaction if money is non-negative input
                     // Create a dummy transaction to carry the money value
                     tx := Transaction{
                          Sender: "Manual",
                          Recipient: "BlockData",
                          Amount: money,
                          Timestamp: time.Now().Format(time.RFC3339Nano),
                     }
                      tx.ID = generateTransactionID(tx) // Generate ID for hashing consistency
                      transactions = append(transactions, tx)
                 }

                // Create the new block structure
                newBlock := Block{
                     Index: lastBlock.Index + 1,
                     Timestamp: time.Now().String(),
                     Transactions: transactions,
                     PrevHash: lastBlock.Hash,
                     Nonce: 0, // Manual blocks have Nonce 0 (no PoW mining)
                }
                 newBlock.Hash = calculateHash(newBlock) // Calculate hash with the transactions and Nonce=0


                // Validate the generated block (it will likely fail the difficulty check if difficulty > 0 and Nonce is 0)
                isValid := isBlockValid(newBlock, lastBlock, difficulty) // Pass difficulty to validation

                if !isValid {
                    // Check ONLY if the calculated hash for THIS block meets difficulty
                    // If it passes difficulty, but isValid is false, something else is structurally wrong.
                    // If it fails difficulty, and isValid is false, that's the expected failure.
                    passesDifficulty := isHashValid(newBlock.Hash, difficulty)

                    if !passesDifficulty {
                         // The block failed the difficulty check, as expected for manual mode if difficulty > 0.
                         // It's structurally valid based on how it was created (index, prevhash, correct hash calc for Nonce=0).
                        fmt.Printf("Manual block %d generated but does NOT meet mining difficulty %d ('%s' prefix). Added anyway.\n", newBlock.Index, difficulty, strings.Repeat("0", difficulty))
                    } else {
                         // isBlockValid returned false, but passesDifficulty is true? This indicates a structural issue
                         // other than difficulty (index, prevhash, or hash calculation mismatch).
                         log.Printf("Manual block %d validation failed for reasons other than just difficulty. Not adding.", newBlock.Index)
                         fmt.Printf("Manual block %d validation failed structurally. Not added.\n", newBlock.Index)
                         goto nextManualInput // Skip adding and go to the next input prompt
                    }
                }
                // Add the block if it was structurally valid (even if it failed difficulty in manual mode)
                Blockchain = append(Blockchain, newBlock)
                fmt.Printf("Added block %d (Manual) with %d transactions (Nonce: %d, Valid PoW: %t)\n", newBlock.Index, len(newBlock.Transactions), newBlock.Nonce, isHashValid(newBlock.Hash, difficulty))


            nextManualInput:
                fmt.Print("Enter Money value (for a dummy transaction) (-1 to stop adding blocks): ")
                _, err = fmt.Scanln(&money)
                if err != nil {
                     log.Printf("Error reading Money input: %v", err)
                     break // Exit loop on error
                }
            }
            fmt.Println("\nManual entry finished.")
            exportBlockchainToJSON() // Export after manual entry is done


        case 2:
            fmt.Println("\n--- Automatic Block Generation & Mining Mode Selected (Single Dummy Transaction) ---")
            fmt.Printf("Mining %d blocks with difficulty %d ('%s' prefix)...\n", totalBlocksToProcess, difficulty, strings.Repeat("0", difficulty)) // Use global var

            numWorkers := 1 // Default to single-threaded
            if miningMode == "b" {
                numWorkers = runtime.NumCPU()
                if numWorkers < 1 { numWorkers = 1 }
            }

            fmt.Printf("Using %d worker(s).\n", numWorkers)

            for i := 0; i < totalBlocksToProcess; i++ { // Use global var
                lastBlock := Blockchain[len(Blockchain)-1]
                randomMoney := rand.Intn(500000) + 1 // Simulate getting new data (random Money)

                 // Create a single dummy transaction for this block to hold the Money value
                 dummyTx := Transaction{
                      Sender: "System",
                      Recipient: "Reward",
                      Amount: randomMoney,
                      Timestamp: time.Now().Format(time.RFC3339Nano),
                 }
                 dummyTx.ID = generateTransactionID(dummyTx) // Generate ID

                 transactionsForThisBlock := []Transaction{dummyTx}


                // --- Mine the block ---
                newBlock := mineBlock(lastBlock, transactionsForThisBlock, difficulty, numWorkers)


                // Validate the mined block (should pass if mineBlock found a valid hash)
                if isBlockValid(newBlock, lastBlock, difficulty) {
                    Blockchain = append(Blockchain, newBlock)
                } else {
                    log.Println("Error: Automatic mining created an invalid block!")
                    break
                }
                 // time.Sleep(100 * time.Millisecond) // Optional pause
            }
            fmt.Println("Automatic mining complete.")
            exportBlockchainToJSON()


        case 3:
            fmt.Println("\n--- Automatic Block Generation & Mining Mode Selected (With Random Transactions) ---")
            fmt.Printf("Mining %d blocks with difficulty %d ('%s' prefix) with random transactions...\n", totalBlocksToProcess, difficulty, strings.Repeat("0", difficulty)) // Use global var

            numWorkers := 1 // Default to single-threaded
            if miningMode == "b" {
                numWorkers = runtime.NumCPU()
                if numWorkers < 1 { numWorkers = 1 }
            }

            fmt.Printf("Using %d worker(s).\n", numWorkers)

            for i := 0; i < totalBlocksToProcess; i++ { // Use global var
                lastBlock := Blockchain[len(Blockchain)-1]

                // --- Generate random transactions for this block ---
                numTransactions := rand.Intn(20) + 1 // 1 to 20 transactions per block
                transactionsForThisBlock := generateRandomTransactions(numTransactions)


                // --- Mine the block ---
                newBlock := mineBlock(lastBlock, transactionsForThisBlock, difficulty, numWorkers)


                // Validate the mined block (should pass if mineBlock found a valid hash)
                if isBlockValid(newBlock, lastBlock, difficulty) {
                    Blockchain = append(Blockchain, newBlock)
                } else {
                    log.Println("Error: Automatic mining created an invalid block!")
                    break
                }
                 // time.Sleep(100 * time.Millisecond) // Optional pause
            }
            fmt.Println("Automatic mining complete.")
            exportBlockchainToJSON()


        default:
            return // Should not happen due to initial mode check
        }
    }() // End of goroutine


    // --- Common Actions (Server, Browser, Stay Alive) ---
    // These run concurrently with the block generation/mining goroutine

	// HTTP server setup
	http.HandleFunc("/", rootHandler) // Use the new custom root handler
	http.HandleFunc("/blockchain", blockchainHandler) // Use the new custom blockchain handler

	// Start the server in a goroutine
	go func() {
		fmt.Println("\nStarting server on :8081. Keep this window open.")
        fmt.Println("View the visualization at http://localhost:8081/")
		err := http.ListenAndServe(":8081", nil)
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
		fmt.Println("Server stopped.")
	}()

	// Give the server a moment to start before opening the browser
	time.Sleep(500 * time.Millisecond)

	// Open the browser
	url := "http://localhost:8081/"
	fmt.Printf("Opening browser to %s...\n", url)
	openBrowser(url)

	// Prevent main from exiting, keeping the server and mining goroutines alive
	select {} // This blocks forever

}