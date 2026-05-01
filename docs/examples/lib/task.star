# lib/task.star — routes task operations to whichever backend is configured.
#
# Change the implementation here to switch task backends globally.
# All intent scripts that load this file are updated automatically.
#
# Usage:
#   load("lib/task.star", "add", "list", "complete")
#   add("Review PR #42", project="work", priority="H")

def add(description, project="", priority="", due=""):
    """Add a task. Routes to the TaskWarrior integration by default."""
    args = {"description": description}
    if project:
        args["project"] = project
    if priority:
        args["priority"] = priority
    if due:
        args["due"] = due
    return task.add(args)

def list(filter=""):
    """List tasks, optionally filtered."""
    return task.list({"filter": filter})

def complete(id):
    """Mark a task as done."""
    return task.done({"id": str(id)})
