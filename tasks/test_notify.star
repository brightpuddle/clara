def main():
    print("Sending notification...")
    tui.notify.send(message = "Non-interactive notification")
    print("Sent!")
    res = tui.notify.send_interactive(
        prompt = "Choose an option:",
        options = [
            "Option 1",
            "Option 2",
        ],
    )
    print("You chose: " + res)
