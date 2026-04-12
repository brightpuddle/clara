def main():
    res = clara.wait(tui.notify.send_interactive(
        prompt = "How are you?",
        options = ["Good", "Bad"],
    ))
    print(res)
