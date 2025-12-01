import threading
import time
from loguru import logger
from app.aoc_client import fetch_leaderboard
from app.slack_notifier import send_slack_message
from app.vestaboard_notifier import send_vestaboard_message

# Dummy formatting functions

show_leaderboard = True

VESTABOARD_CHAR_MAP = {
    ' ': 0,
    'A': 1, 'B': 2, 'C': 3, 'D': 4, 'E': 5, 'F': 6, 'G': 7, 'H': 8, 'I': 9, 'J': 10, 'K': 11, 'L': 12, 'M': 13, 'N': 14, 'O': 15, 'P': 16, 'Q': 17, 'R': 18, 'S': 19, 'T': 20, 'U': 21, 'V': 22, 'W': 23, 'X': 24, 'Y': 25, 'Z': 26,
    '1': 27, '2': 28, '3': 29, '4': 30, '5': 31, '6': 32, '7': 33, '8': 34, '9': 35, '0': 36,
    '!': 37, '@': 38, '#': 39, '$': 40, '(': 41, ')': 42, '-': 44, '+': 46, '&': 47, '=': 48,
    ';': 49, ':': 50, "'": 52, '"': 53, '%': 54, ',': 55, '.': 56, '/': 59, '?': 60, '°': 62
}

# data from API, timestamp in UTC
def get_latest_stars(data, timestamp):
    results = {}
    for member in data['members'].values():
        if member['last_star_ts'] * 1000 > timestamp: # * 1000 because it is coded to 1970
            logger.info(f"Going to find a new star for {member['name']}")
            for day in member['completion_day_level']:
                obj = member['completion_day_level'][day]
                if "2" in obj and obj["2"]["get_star_ts"] * 1000 > timestamp: # * 1000 because it is coded to 1970
                    results.update(
                        {
                            member['name']: {
                                day: "GOLD"
                            }
                        }
                    )
                elif "1" in obj and obj["1"]["get_star_ts"] * 1000 > timestamp: # * 1000 because it is coded to 1970
                    results.update(
                        {
                            member['name']: {
                                day: "SILVER"
                            }
                        }
                    )
        else:
            logger.info(f"No new stars for {member['name']}, because {timestamp} > {member['last_star_ts'] * 1000}")
    return results

def create_header(text) -> list[int]:
    title_codes = [VESTABOARD_CHAR_MAP[x] for x in text]
    # Pad to 20 chars if needed
    title_codes = (title_codes + [0]*(20-len(title_codes)))[:20]
    title_row = [64] + title_codes + [67]
    return title_row

def vesta_latest_stars(stars):
    title_row = create_header("  OUR LATEST STARS")

    def entry_codes(entry):
        print(entry)
        return [VESTABOARD_CHAR_MAP.get(x, 0) for x in entry]

    # Two yellows for GOLD or one yellow for SILVER
    entries = []
    for developer in stars:
        name = developer.replace(" ", "")[:7].upper()
        for day in stars[developer]:
            listone = entry_codes(f"{name} DAY {day} ")
            listtwo = [65, 65] if stars[developer][day] == 'GOLD' else [65, 0]
            entries.append(
                listone + listtwo
            )
    # chunk per 5
    chunks = [entries[x:x+5] for x in range(0, len(entries), 5)]
    all_pages = []
    empty_line = [0]*19
    for chunk in chunks:
        all_pages.append([
            title_row,
            [67, 0] + chunk_or_empty(chunk, 0, empty_line) + [64],
            [64, 0] + chunk_or_empty(chunk, 1, empty_line) + [67],
            [67, 0] + chunk_or_empty(chunk, 2, empty_line) + [64],
            [64, 0] + chunk_or_empty(chunk, 3, empty_line) + [67],
            [67, 0] + chunk_or_empty(chunk, 4, empty_line) + [64],
        ])
    if len(all_pages) == 0:
        all_pages.append([
            create_header("  NO NEW STARS :("),
            [67, 0] + empty_line + [64],
            [64, 0] + empty_line + [67],
            [67, 0] + empty_line + [64],
            [64, 0] + empty_line + [67],
            [67, 0] + empty_line + [64],
        ])
    return all_pages

def chunk_or_empty(chunk, index, empty_line):
    if index < len(chunk):
        return (chunk[index] + [0]*(19-len(chunk[index])))[:20]
    else:
        return empty_line

def format_leaderboard(data):
    members = sorted(data['members'].values(), key=lambda m: m['local_score'], reverse=True)
    lines = []
    prev_score = None
    for i, m in enumerate(members[:10]):
        score = m['local_score']
        rank = str(i) if score != prev_score else ' '
        name = m.get('name', 'Anon').replace(' ', '')[:7]
        if i < 5:
            lines.append(f"{rank} {name}")
        else:
            lines.append(f"{name} {rank}")
        prev_score = score
    return lines    

def vesta_leaderboard(entries):
    title_row = create_header("ADVENT OF CODE 2025!")
    def entry_codes(entry):
        # Truncate/pad to 10 chars, convert to uppercase
        entry = (entry or '')[:10].upper().ljust(10)
        return [VESTABOARD_CHAR_MAP.get(x, 0) for x in entry]

    # Pad entries to 10 items if needed
    padded_entries = (entries + [''] * 10)[:10]

    return [
        title_row,
        [67] + entry_codes(padded_entries[0]) + entry_codes(padded_entries[5]) + [64],
        [64] + entry_codes(padded_entries[1]) + entry_codes(padded_entries[6]) + [67],
        [67] + entry_codes(padded_entries[2]) + entry_codes(padded_entries[7]) + [64],
        [64] + entry_codes(padded_entries[3]) + entry_codes(padded_entries[8]) + [67],
        [67] + entry_codes(padded_entries[4]) + entry_codes(padded_entries[9]) + [64]
    ]

def format_star_updates(old, new):
    # Dummy: just count stars
    old_stars = {k: v['stars'] for k, v in old['members'].items()}
    new_stars = {k: v['stars'] for k, v in new['members'].items()}
    updates = []
    for k in new_stars:
        if new_stars[k] > old_stars.get(k, 0):
            updates.append(f"{new['members'][k].get('name', 'Anon')} got a new star!")
    return updates

# Scheduler logic
class Scheduler:
    def __init__(self):
        self.last_leaderboard = None
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def run(self):
        while True:
            try:
                data = fetch_leaderboard()
                logger.info(data)
                logger.info(format_leaderboard(data))
                if show_leaderboard:
                    send_vestaboard_message(vesta_leaderboard(format_leaderboard(data)))
                    show_leaderboard = False
                else:
                    send_vestaboard_message(vesta_latest_stars(get_latest_stars(data, int(time.time() - 7200)*1000))[0])
                    show_leaderboard = True
                # if self.last_leaderboard:
                #     # Star updates & Vestaboard every 15 min
                #     updates = format_star_updates(self.last_leaderboard, data)
                #     if updates:
                #         send_slack_message("\n".join(updates))
                # else:
                #     print("First run, skipping star updates.")
                # self.last_leaderboard = data
                # # Leaderboard to Slack daily at 6:00
                # if time.localtime().tm_hour == 6 and time.localtime().tm_min == 0:
                #     send_slack_message(format_leaderboard(data))
            except Exception as e:
                logger.error(f"Scheduler error: {e}")
            time.sleep(900)  # 15 minutes

scheduler_instance = None

def start_scheduler():
    global scheduler_instance
    if scheduler_instance is None:
        scheduler_instance = Scheduler()
