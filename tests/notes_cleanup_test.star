# tests/notes_cleanup_test.star

def test_vec_version():
    res = db.query(sql="SELECT vec_version()")
    print("Vec version: " + str(res[0]["vec_version()"]))

def main():
    test_vec_version()

clara.task(main)
