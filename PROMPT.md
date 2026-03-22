# Context

Now that we have a basic framework for Clara, I'd like to develop a roadmap and
concrete steps to move forward from here. My goal from this planning session is
to develop a series of iterations to accomplish the stated goals, that can
either individually be assigned to agents in build mode or to plan mode to delve
into more detailed planning for the specific task. I would like to work through
opencode with AI over the next couple of days to accomplish as much of this as
possible.

# Ask (your task)

You will NOT be writing code. You will be writing markdown files. I DO NOT
expect a single implementation plan. This is a high-level planning session. The
output will be a series of prompts to be provided back to my agents in opencode
to implemented or for further, focused feature planning.

Please do the following:

1. Identify key architectural decision points and provide guidance and ask
   questions within this planning session to help me identify the big, pivotal
   choices. This is particularly important when these are core functionality to
   clara that will impact the other work I have planned.
2. Break this into a series of task items that I can provide to opencode in
   either plan or build mode, depending on the task, as follows:

- These task items should be prioritized and written to markdown files in the
  TASKS folder in this repo.
- Priorities should take into account both the priority I've expressed in the
  following writeup and dependencies between tasks.
- Tasks should not generally be broken up by size or effort (claude-sonnet-4.6
  is quite capable of large, difficult tasks), unless you have strong reason to
  do so. They should generally be separated by solution or task. For example,
  creating a new, simple intent and a complete TUI refactor/recreate are likely
  both one task, even though these are substantially different in effort and the
  latter would probably benefit form its own planning session.
- The markdown filenames should be prefixed with a number to keep them in the
  correct order based on how you determine priority, then a title separated by
  hyphens, e.g. the task to create an intent to use embeddings to look for
  backlink opportunity in my obsidian notes, might be called
  `010-zk-backlink-intent.md`
- The markdown should be prefixed with yaml frontmatter. The frontmatter should
  have a boolean field `plan_recommended` indicating whether this task should
  have its own planning session, or if it's ready to implement. If it requires
  planning, the open questions or focus of planning should be highlighted in a
  `Planning Context` header in the prompt.
- The body of the markdown should contain the key context for that task from
  this prompt and from any subsequent, high-level planning we do or decisions we
  make.

My intent is that this is a broad, first-pass, meta-planning session, which will
produce a large number of tasks, many of which can be implemented immediately,
and some of which will require more detailed planning or important architectural
choices.

# Vision

This vision of this project is to automate as much of myself as possible. I
would like to leverage automation as much as possible to ease my own load,
cognitively and emotionally. I would like to apply this to all places where I
use my digital footprint: in work and in my personal life. I have tangible,
human things to do, e.g. for work, meeting and consulting with other engineers,
and in my personal life, home improvement, spiritual development, physical
fitness, and time with family. My goal with Clara is to reliably, consistently,
and efficiently automate everything that can be automated.

A core philosophy differentiating Clara from other solutions is that I do not
believe AI should be the central workflow engine. AI is a powerful tool, but it
is not deterministic, and it is not always the most efficient way to accomplish
a task. You could consider CI/CD actions as an example of the Clara philosophy.
AI may create the actions and AI may be a step in the actions, but the action
itself is a deterministic workflow.

# Project Goals

## Reliability and Repeatability

We will use AI extensively, and this is large part of this solution, but our
goal is to establish deterministic workflows wherever possible. This solution
differs distinctly from the Claw family of solutions in that the core
interpreter is deterministic and does not inherently _need_ to use AI. This
makes the system much more efficient and reliable and durable, stateful
repeatability. We will use AI as one of our tools, not as a core interpreter or
workflow engine.

This philosophy should be considered in all design. For example suppose we are
organizing my filesystem using AI both to determine my existing organization and
to decide where to put any particular file. The organizational heuristics could
be determined by AI, through a markdown prompt, but this would be run _once_,
and then could be codified into a ruleset. This could be its own intent that
watches the markdown prompt for changes. Some files could potentially be
categorized immediately based on rulset, e.g. "unsubscribe" or messages
addressed to the wrong person, and others would go through vector search for
categorization.

As compared to a pure-AI solution, I could inspect and improve this ruleset over
time, or re-run the prompt and diff the rules it generates. It's much more
efficient, reliable, and inspectable.

## Efficiency

It's important that Clara remains fast and efficient as it grows. System
resource monitoring and scheduling should be a core part of the design and not
an afterthought or reliant on defensive programming in starlark intents.

## Autonomous Development

Clara and AI should be as productive as possible, even when I'm sleeping or
doing other things. Much of my existing technical work is bound my own time.
Clara can work around the clock, making the time I have more productive and
focused.

## Prioritization and focus (reduce cognitive load and emotional burden of information overload and limited time)

When I sit in front of my computer, I should quickly be able to see what is most
urgent.

Clara will be my central HUD which helps me focus on what needs my attention.
Incoming requests or demands on my time will come from a diverse array of
sources, e.g. calendar events, email, webex, my todo lists (apple reminders or
taskwarrior), TODO or `- [ ]` tasks that I've placed in my notes, github issues,
notifications from other apps. High priority, urgent items may also generate a
sticky MacOS notification.

When I look at a task in my TUI, this should already be handled or triaged as
much as reasonably possible. An email, webex, or github response should be
pre-generated for my editing and/or approval when possible. If there are
questions, I should be presented with Q&A. If research needs to be done, it
should already be done. When it's appropriate, these can be handled
automatically. Related information from across my digital ecosystem should be
present and available to me with a means to quickly open and reference these
resources.

When something does require my attention it should require as little effort as
possible.

## Data Janitor

I have many cleanup/organizational tasks that can easily be automated. These
should be worked on in the background. These are not real-time requirements, but
should be background workers that constantly, progressively improve, through the
use of AI and declarative workflows. My existing organizational systems and
heuristics should be learned and applied, and as these evolve, these should be
able to learn from me.

Some examples of this:

- Hide/unhide webex groups that do not need to be visible to me and triage
  "mailing list" type groups or bot messages
- Clean up emails, e.g. archive, categorize, add to a delete folder for human
  review/delete. Move progressively towards inbox-zero.
- Clean up files by deleting or sorting into my existing structures. I have a
  strong organization system for code, work, personal across various locally
  replicated cloud storage. When working on projects, things get scattered
  around. This should be organized for me
- I keep my notes in Obsidian/zk/markdown. Some of these are zettelkasten, some
  are journal entries, and some are just quick notes. There's a powerful
  opportunity to identify and add backlinks, tags, aggregate multiple notes,
  organize the files, etc. There's a wealth of untapped insight in this, both
  personally, and professionally.
- Cleanup unused apps, unused homebrew projects, leftover junk for uninstalled
  apps in `~/Library`, ~, `~/.local`, `~/.config`, and other system folders.

## Continuous improvement

### Software projects

I currently have ongoing responsibility for a number of automation projects.
There are a variety of things that could be done for these projects, e.g.
working on github issues, ideas that I've written about in my notes or that
people have mentioned to me in webex or email, code quality (linting,
architectural best practices, test coverage). These projects are in an
enterprise github account that is on github.com, are configured with the github
MCP server and gh cli client.

I would like ongoing review and improvement on these projects, triaging existing
issues and centralizing and opening new issues when the requests or ideas come
from other sources. Github issues are sometimes directly actionable and
assignable to copilot, or can be with internet research.

### Sysadmin

I run a web app on a production server, which is also one of my automation
projects. This server is running through docker compose as a series of docker
containers on a prod and pre-prod openstack, ubuntu VM provided by Cisco IT.
This has management APIs and SSH access from my laptop when inside the Cisco
VPN. This server needs updates, is not monitored for CPU,  
memory, or disk space, and the database is not backed up. I've considered
installing a solution for this or moving to nixos or creating a system
management process. I find it difficult to find time for all of this.

I do not want AI to give me a list of tools to research, download, configure,
and install, or system tasks to work on. I have SSH and API access to this
server--Clara and AI can review, advise, and do this work for me.

### My own workflow

I have a highly optimized, terminal-centric workflow. I use Wezterm, zsh,
neovim, opencode, yazi, homebrew, uv, chezmoi, fzf, and many other CLI tools. I
use Raycast, mostly for quick app switching, Chrome for internet, Apple Mail and
Calendar, Webex, 1password, and a variety of other apps. My configuration is all
in my dot files. My folder use is recorded by zoxide, and commands history in
zsh. Screentime has app usage data. There are at least two different
opportunities here:

1. Continuous improvement of my own workflow, e.g. newer, better tooling,
   enhancements to streamline my workflow, fix errors, bugs, or
   misconfigurations in my system configuration (dot files). I expect much of my
   need for optimization may evaporate as Clara performs more of the work for
   me, but I also want to stay on top of the latest tooling, e.g. node vs bun,
   pyright vs basedpyright, bat vs ls, neovim plugins, and so on. Let me know if
   it can be better, advise me on this, and if I approve, make it better.
2. Cleanup and simplification as mentioned earlier in the data-janitor section,
   e.g. cleaning old app data from removed apps, identifying unused apps or
   homebrew taps. The MacOS `~/Library` folder and dot files in the home folder
   suffer from sprawl. Nix seems promising to help with this, but it's a
   difficult uplift as compared to the broad scope and ease of use of homebrew.
   If I were to clean these manually, I'd have to search through various
   locations and look up what different things are, determine if I'm still using
   them (are they even installed), and then decide if I can remove them.

As I come up with new ideas for additional automation, I would like to present
them to an AI agent which will triage and categorize this, e.g. requires a new
Clara MCP server, a new intent, a new ClaraBridge build, etc. The translation of
idea into concrete solution should occur within the context of everything that
already exists (Clara, my ecosystem, my historical data, my vision for my
digital life), and this new capability should be implemented mostly through
guided Q&A.

### Logging

Much of my behavior is already logged, e.g. zoxide, zsh history, Screentime,
browser history, deleted emails, replied emails, webex messages. I would like to
fill in the gaps and expand logging where possible to continuously record my own
behaviors in work, which will then be used to identify opportunities for
workflow improvement and automation. Many of my expectations of Clara and of AI
are based on an understanding of me. I believe we have a huge wealth of
historical data to be progressively analyzed, but where we don't, I would like
to ensure that we do a better job of collecting this moving forward. I would
like a process or processes that continues to learn my systems and providing
solutions to my challenges, which requires insights into what these are.

Data should not grow endlessly, but should be kept for long enough to learn from
and to allow continuous improvement of heuristics.

### Self-improvement

The internet provides a vast array of automation ideas. AI is evolving rapidly
and people are putting it to use in new ways. The Claw tools are also "automate
everything" frameworks, albeit with a different core philosophy and design. We
should learn from what others are doing to identify opportunities for
improvement and new capabilities. This itself should be an intent that does
periodic research and provides suggestions.

## My wife's workflow (Alex)

I will be installing Clara on Alex's laptop, an M2 Macbook w/ 8GB of memory. She
has some overlapping requirements and some unique requirements. She is a good
proving ground for exploring releasing Clara generally.

- She is non-technical and does not work in the terminal. I can provide her the
  terminal and clara TUI, but she will not regularly be working in it unless I
  give her a task (go here to ask a question, to find data). It **might** be
  necessary to create a web interface for her, that would interface with clara
  and provide a subset of the TUI capabilities. If we do create this, it would
  not be a core part of the project (at least for now, unless I decide to add
  web), but a one-off solution.
- She does not understand most technical questions and will simply ignore things
  she does not understand, and so communication to her must be very clear,
  pointed, and non-technical
- Her filesystem, Downloads, Documents, Desktop, Pictures, and gmail is a mess.
  There are several attempts at structure and organization in different folders,
  but none are used consistently and some overlap (she might have a "Dogs" or
  "Home Improvement" folder in three different locations, and also have a bunch
  of emails with photos she likes that she emailed herself on home improvement.
  It's very important that she can locate important things and so all of these
  are important, but it's very difficult for her to find things with them
  scattered all over.
- She has a unique need for Facebook marketplace automation. This is a HIGH
  priority, and I would like to get a working prototype for her to try out in
  the next day or two. This is much more important than organizing her files.

### Facebook marketplace automation

#### Overview

We have a 12 y/o granddaughter and Alex has been buying quality children's
clothes from thrift stores since our granddaughters birth. We have a lot of
girl's clothing, shoes, and addition to the clothing, she sells "garage sale"
type things, like dog jackets, leashes, various things around our house. She has
an eye for brands and quality and so most of these are purchased at an extreme
discount and priced well below value, e.g. she might spend $1-5 for a shirt in
near new or new condition, that retails for $50-70 and might sell it for $10-20.
I do not know her exact margins, but this is an example.

#### Specifics

- She has approximately 250-300 active ads at a time.
- Ads age and nobody sees old ads. You can "renew" an add a few times (3 times
  or so) and then you can delete/relist. Both of these functions are buttons in
  facebook.
- !! IMPORTANT: Facebook automation should follow human behavior, e.g.
  randomized delays, throttling new ads, to avoid triggering facebook's spam/bot
  policies. We are not trying to generate high-volume listings, but just remove
  the manual burden

#### Automation tasks

##### New ads

Her current workflow for a new ad is as follows:

1. Take a photo with her iphone
2. Move to her computer, open photos app
3. Put photos into google image search, gemini, or chatGPT to identify item and
   value
4. Create a new ad

- Choose a discounted price
- Choose a title with important information such as size (some people do not
  read the details and ask unhelpful questions)
- Complete other options like category, condition, etc.
- NO SHIPPING; door pickup
- Create a concise description with important information
- add photos

The automation will work (roughly) as follows:

1. She takes photos with her iphone and moves these into an inbox album
2. Once synced to icloud (available on her computer running clara), these can be
   noted (added to a sqlite table) and moved out of the album
3. pictures will be researched to determine what the item is, and cost of item
   or similar items and to provide a title, description, and discounted cost.

- This should be done with pre-built prompts so that additional parameters,
  language, requirements can be added to tune this behavior
- Gemini free tier is probably a good option for this

1. Facebook does not provide APIs to non-business users, so we will use a Chrome
   MCP extension (e.g. <https://github.com/hangwin/mcp-chrome>). Either an
   existing one of if there's one of sufficient quality and that meets our
   needs, or we will create one and add it to the Clara repo.

##### Renew ads

Her currently workflow for renewing ads is as follows:

1. Considering 10 day weather forecast, season, holidays, school, day of the
   week, she chooses to renew/relist (you can renew a few times, and then get
   switched to the option to relist, which brings the item to the top of the
   view). She currently has to search or scroll through hundreds of items to
   find relevant ones to relist. For example, people always shop more on Friday
   and Saturday. If it's going to be 85 degrees, people will be more interested
   in shorts, tshirts and swimsuits, but not long sleeves. In December,
   Christmas things will be more in demand.

Automation workflow:

1. Renewal factors are defined in human text for AI, and relevant data is
   gathered, e.g. factors might be described how I just described them in the
   previous workflow sentence, and then AI would lookup the actual weather
   forecast on a daily schedule. As this is real time data, this could use
   gemini free tier
2. Existing ads would be read using the chrome extention and sent to an ollama
   model on mac mini (to avoid overrunning gemini free tier), along with
   relevant renewal prompt and collected data. AI would respond with whether it
   should be renewed or not, and justification.
3. Potential renewals require her approval. They should be presented to her with
   a title, the photos (she primarily looks at photos to know what the items
   is), and justification for why it should be renewed. She should easily be
   able to say "yes/no", e.g. press 1 for yes, 2 for no, 3 for other/comment
4. The other/comment is a immediate interaction with the model and with the
   database of recommendations and prompt, e.g. if she types "do not show me any
   more swimsuits, because the box where the swimsuits is in is too hard to get
   to", it should immediately work to filter out swimsuits from her
   recommendation list. It should show a working indicator and update the list
   to remove all swimsuits. She should not have to wait until the next
   run/tomorrow to change this. This could be performed through an immediate
   trigger of the clara worker intent with the added high-priority context of
   her comment.
5. Once approved, browser-based MCP takes over and performs her approved actions

##### Reply to marketplace messages

Facebook provides people suggested questions that they can ask just by clicking
a button, which means the seller often gets waste-of-time questions, like "is
this item still available" or "what size is this," which are all right there in
the ad.

Automation workflow:

1. Clara will monitor for new FB messages. Ideally a FB messenger MCP server,
   but if not possible, we will automate the browser with MCP
2. When a message relates to a marketplace add, the ad context and a prompt
   defining response criteria should create a messenger response and a
   confidence level that this is a generic question.
   - If it's a generic question or easily answerable (high confidence), clara
     should respond directly
   - If there's lower confidence, e.g. the user is engaging in more
     interpersonal conversation or there's a question that isn't simple/obvious,
     AI should respond with a human-sounding excuse, e.g. "I'm at the store
     right now, but I'll reply when I get back." and then marked as UNREAD so
     that she sees these when she goes into messenger

#### Requirements

- All facebook automation should be throttled and randomized to make it appear
  human, and avoid triggering facebook anti-spam/bot policies. We are not trying
  to generate high-volume posts; only provide human assistance
- If possible, facebook automation should be non-blocking and occur in the
  background, i.e. we do not want to prevent her from using her browser while
  automation is running
- The UI that asks for approval must show pictures for context. I'm undecided on
  installing ghostty so she has images in the terminal and putting her into the
  TUI vs just creating a simple custom webserver (echo, templ/htmx and/or svelte
  components if a greater degree of interactivity is required, tailwind,
  daisyui) that connects to clara in the same way as the TUI and provides the
  necessary subset of the TUI capabilities, including her approval workflow. I'm
  leaning towards the latter for ease of use for her.
- The prompts for parameters around ad generation should be easily updatable.
  The prompts should be stored in markdown files, monitored for changes, and
  read in by the intent as opposed to being hardcoded into the intent as a
  triple-quoted multiline string. This way she can just edit a markdown file
  instead of code.
- She has an M2 Macbook with 8GB of memory, and so is severely limited in what
  she can do locally with ollama. AI should be performed through a combination
  of the Mac mini (local network, M4, 64GB of memory), and free services, e.g.
  gemini, chatGPT, etc. Resource availability (M4 is busy, gemini has exhausted
  free requests for the day) should be handled gracefully. I believe a tiered
  model is most appropriate, where gemini handles the new ad request and ollama
  on the mac mini handles reviewing renewals and responding to chats, but I'm
  open to suggestions. This is not expected to be a huge volume of requests, and
  with the gemini context window, renewal review could be batched into a single
  large prompt. I'm also already a google one subscriber and have five google
  home mini devices, and I'm completely open to a gemini plus or pro plan.

# UX

I would like to be able to accomplish the following from within my TUI:

1. See what needs my attention / priority across all known sources, e.g. an
   upcoming calendar event, time-gated request from someone or verbal promise I
   made, task in reminders
2. Receive suggestions or guidance on accomplishing tasks, similar to planning
   questions in agentic development clients, e.g. options, recommendations,
   suggested responses to email, webex, github issues, links to URLs or
   resources to accomplish my tasks, proposed agentic development tasks. These
   should be in Q&A structure, where I'm presented with options that I can
   choose with a number, with an option to provide my own response or enter into
   discussion on this question, the same as how opencode does this.
3. Ask questions on my data, e.g. "Can you find my ACI lab resources?", "What
   are my top priorities for next week?", "What book do you suggest I read next
   based on my spiritual and personal journey?"

# Technical

Clara is still early in adoption and backwards compatibility is not a concern,
e.g. in the TUI refactor, we can completely replace what exists.

## TUI refactor

I would like to do a complete rebuild/refactor of the TUI, keeping absolutely
nothing from the current design. Now that I have a better vision of my
requirements, I have a better understanding of where the TUI fits into this. I
would like to copy the opencode design almost verbatim. Instructing AI to do UI
development is challenging, because unlike functional behavior, there's an
aesthetic and behavioral element to UX that is difficult to test
programmatically. Opencode is rust/typescript, but I believe using the repo as a
reference and translating this to bubbletea, and testing where possible, we can
duplicate both the appearance and behavior. Opencode is a _very_ well designed
UI, both attractive and functional. Some UX examples of this.

- Opencode provides a strong Q&A model with context and suggestions in the top
  panel and a prompt area to answer or provide feedback in the bottom. Types of
  data are clearly separated in the top panel. This will provide a clear means
  of triaging suggestions and prioritization to me (the user)
- The AI prompt allows me to ask questions about my data as described earlier,
  and the content area provides a clean display for this. Opencode
  "intelligently" collapses certain content in the UI and puts other content in
  the background (agent logs)
- The aesthetics are very refined, including the two tone shading, the modals,
  the integrated settings, click to expand, the highlight on the left of
  selected items, the scrollbars using block ASCII. I would to duplicate both
  appearance and behavior without having to progressively prompt AI to make
  incremental improvements. The opencode design should be adhered to as
  precisely as possible.
- Opencode presents a different splash screen as compared to the working screen,
  which is not necessary
- When the viewport is wide enough, opencode presents a sidebar with model
  usage, which is not necesssary, as model usage is not a central function of
  Clara. This space could be used to cycle through other useful information,
  e.g. top priorities, related context, intent status, etc. As this area
  collapses depending on the viewport, this will not be the exclusive location
  of any information, but may be helpful for important information when
  available.

## MCP provider

The TUI uses a custom IPC upgrade to attach to clara through MCP. What I like
about this is that security is simple and strong; it's handled by access to the
socket file by the OS, the same as e.g. docker.

I'm considering modifying or adding to this to allow standardized MCP from other
clients, either locally or over the network. This presents new challenges and
design choices with security, but also opens up opportunities for integration.

For example:

- If MCP were available locally, I could add select clara tools to Raycast or
  opencode.
- If MCP were avilable over the network, I could connect the clara instance on
  my my or Alex's computers with an instance on the mac mini, allowing use of
  LLM through clara instead of through an nginx reserve proxy or custom gateway.

The security aspect of this is a big design choice and is an important
consideration. Local could potentially be provided over stdio with per-app
tokens. Network probably suggest oauth2.1, or at least an https gateway with
bearer tokens.

## LLM utilization

There are many demands on AI within this solution, e.g. reading one of my emails
or webex messages, reading a github issue, reading one of Alex's marketplaces
messages from messenger. We both have multiple options to us and there are a
number of potential demands on the Mac Mini.

### Mac mini memory utilization

The Mac mini must protect its resources. Ollama already does requests to the
same model in serial, but it only blocks until the first one is complete (it
provides no feedback). If multiple workflows or Alex and I use different,
larger-sized models on the mac mini, it will start multiple instances, and could
easily exhaust the memory. Currently ollama is running over an nginx reverse
proxy, but I believe this should be provided as a network MCP service so that
resource protection and model selection can be built in.

This could be a custom ollama gateway exclusively for this purpose and not a
core part of Clara, or the mac mini could run clara and clara's _provided_ MCP
servers could be network enabled and perform the same resource protection
locally or remotely. The first solution has the benefit that it does not require
a general architectural uplift to clara; the second has the advantage that it
applies anywhere LLM services are available through clara, i.e. local LLM
resources on my macbook and remote LLM resources on the mac mini would both be
protected by the same code in the same Clara LLM MCP server.

I am very interested in the potential with this, but I'm unsure of the best
design and am open to suggestions.

### Mac mini security

This is not a huge issue as my local network is already secure, but my current
nginx proxy is wide open, allowing open access to ollama. If the mac mini ran a
custom gateway or clara as an MCP network service and exposed this on a port,
this would minimally provide similar access to ollama and maybe more if other
tools are exposed.

### LLM Multiplexing

I have multiple LLM options available to me:

- ollama on 64GB M4 mac mini
- copilot subscription
- free gemini (and I'm strongly considering subscribing, which I can share with
  my family (Alex))

Alex has multiple LLM options available to her:

- ollama on mac mini
- gemini (free tier unless I decide to subscribe)

I would like to modify the built in ollama mcp server to be an `llm` MCP server,
and to connect to all of these and maybe more. I would like to push resource
handling into the MCP server, so a tool call can make a request to either
specific service, or a specific type of request and the LLM MCP server will
handle selection. For example, if I made a request to "llm.generate" I could
specify something like `general-small` or `trivial` and this could choose
between general purpose smaller models (qwen3 on ollama, gemini-2.5-flash on
google, gpt-5-mini on copilot). I realize these specific models are not exactly
comparable, but the point is that a certain category of request could
automatically distribute based on usage and availability. This could also help
ensure that Alex and I _typically_ use the same models for the same purpose on
the mac mini, allowing for better resource utilization.

For tasks such as Alex's new Facebook marketplace ads, these could be specified
to specifically use gemini. For tasks such as reviewing my code, this could
explicitly use copilot. For embeddings or triaging files or emails, this could
specifically use ollama.

### Safety and Backout

Many actions are semi-destructive, e.g. deleting an email, deleting a reminder,
moving files, renaming files. I say "semi" because email has trash, reminders
has a trash, files can be renamed and moved back, etc. This is not really a
feature of clara so much as a consideration re intent design.

- Intents that perform non-reversible, higher risk changes such as sending an
  email should ask the user for approval, e.g. in the TUI. Alex's new ad
  workflow also addresses this by creating drafts before posting.
- Most intents should run as autonomously as possible, e.g. file cleanup can
  move files and should be logged in case the user wants to reverse the change.
- Intents should aim to provide on demand tasks for backout in cases where this
  is more likely. In other cases, the user could request this through the chat,
  which has full MCP tool access.
