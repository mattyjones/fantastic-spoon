import os
import time
import json
import random
import logging
import argparse
from typing import List, Dict, Any, Optional
from datetime import datetime

import requests

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

class RateLimiter:
    """Simple rate limiter to ensure we don't exceed X requests per second."""
    def __init__(self, requests_per_second: float = 1.0):
        self.delay = 1.0 / requests_per_second
        self.last_call = 0.0

    def wait(self):
        elapsed = time.time() - self.last_call
        if elapsed < self.delay:
            time.sleep(self.delay - elapsed)
        self.last_call = time.time()

class ISBNDBClient:
    """Client for interacting with the ISBNDB API v2."""
    BASE_URL = "https://api2.isbndb.com"

    def __init__(self, api_key: str, plan_batch_limit: int = 100):
        self.api_key = api_key
        self.batch_limit = plan_batch_limit
        self.limiter = RateLimiter(1.0)  # Standard limit is 1 req/sec
        self.headers = {
            "Content-Type": "application/json",
            "Authorization": self.api_key
        }

    def _request_with_retry(self, method: str, endpoint: str, data: Optional[Dict] = None, retries: int = 5) -> Optional[Dict]:
        """Executes HTTP request with exponential backoff and jitter."""
        url = f"{self.BASE_URL}{endpoint}"
        
        for attempt in range(retries):
            self.limiter.wait()
            
            try:
                response = requests.request(method, url, headers=self.headers, json=data, timeout=30)
                
                # Success
                if response.status_code == 200:
                    return response.json()
                
                # Rate limited
                if response.status_code == 429:
                    wait_time = (2 ** attempt) + random.random()
                    logger.warning(f"Rate limited (429). Retrying in {wait_time:.2f}s...")
                    time.sleep(wait_time)
                    continue
                
                # Other errors
                response.raise_for_status()
                
            except requests.exceptions.RequestException as e:
                wait_time = (2 ** attempt) + random.random()
                logger.error(f"Attempt {attempt + 1} failed: {e}. Retrying in {wait_time:.2f}s...")
                time.sleep(wait_time)

        logger.critical(f"Failed to complete request to {endpoint} after {retries} attempts.")
        return None

    def lookup_isbns_batch(self, isbns: List[str]) -> List[Dict]:
        """Uses the POST /books endpoint to look up multiple ISBNs at once."""
        payload = {"isbns": ",".join(isbns)}
        result = self._request_with_retry("POST", "/books", data=payload)
        
        if result and "books" in result:
            return result["books"]
        return []

def process_file(input_path: str, output_path: str, api_key: str, batch_size: int):
    """Main processing loop."""
    if not os.path.exists(input_path):
        logger.error(f"Input file not found: {input_path}")
        return

    # Load ISBNs
    with open(input_path, 'r') as f:
        isbns = [line.strip() for line in f if line.strip()]
    
    total_isbns = len(isbns)
    logger.info(f"Found {total_isbns} ISBNs. Processing in batches of {batch_size}...")

    client = ISBNDBClient(api_key, batch_size)
    all_results = []

    # Batch processing
    for i in range(0, total_isbns, batch_size):
        current_batch = isbns[i : i + batch_size]
        logger.info(f"Processing batch {(i // batch_size) + 1} (ISBNs {i} to {i + len(current_batch)})...")
        
        batch_results = client.lookup_isbns_batch(current_batch)
        all_results.extend(batch_results)

    # Save results
    output_data = {
        "metadata": {
            "processed_at": datetime.now().isoformat(),
            "total_requested": total_isbns,
            "total_found": len(all_results)
        },
        "books": all_results
    }

    with open(output_path, 'w', encoding='utf-8') as f:
        json.dump(output_data, f, indent=2, ensure_ascii=False)
    
    logger.info(f"Successfully processed {len(all_results)}/{total_isbns} ISBNs. Results saved to {output_path}")

def main():
    parser = argparse.ArgumentParser(description="ISBNDB Bulk Lookup Tool")
    parser.add_argument("input", help="Path to the text file containing ISBNs (one per line)")
    parser.add_argument("-o", "--output", default="results.json", help="Output JSON file path (default: results.json)")
    parser.add_argument("-k", "--key", help="ISBNDB API Key (overrides ISBNDB_API_KEY env var)")
    parser.add_argument("-b", "--batch", type=int, default=100, help="Batch size (Academic: 10, Basic: 100, Pro: 1000)")

    args = parser.parse_args()

    api_key = args.key or os.getenv("ISBNDB_API_KEY")
    if not api_key:
        logger.error("API Key must be provided via --key or ISBNDB_API_KEY environment variable.")
        return

    process_file(args.input, args.output, api_key, args.batch)

if __name__ == "__main__":
    main()