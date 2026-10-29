import unittest
from launch import prepare_config

class LaunchTest(unittest.TestCase):
    def test_occupied_gpu_and_context(self):
        state, config = prepare_config({"gpus":3,"ctx":65536}, {"args":["--max-context","131072"]}, [(0,2000),(1,24000),(2,24000),(3,24000)], "private-key")
        self.assertEqual(state["gpu_ids"], [1,2,3])
        self.assertEqual(config["gpu"], [1,2,3])
        self.assertEqual(config["args"][config["args"].index("--max-context")+1], "65536")
    def test_long_context(self):
        for context in (196608, 262144):
            state, cfg = prepare_config({"gpus":4,"ctx":context}, {"args":["--max-context","131072"]}, [(i,24000) for i in range(4)], "key")
            self.assertEqual(state["ctx"], context)
            self.assertEqual(cfg["args"][cfg["args"].index("--max-context")+1], str(context))
    def test_insufficient_gpus(self):
        with self.assertRaises(ValueError):
            prepare_config({"gpus":4}, {"args":[]}, [(0,1000),(1,24000),(2,24000),(3,24000)], "key")
    def test_single_gpu_removes_split(self):
        _, config = prepare_config({"gpus":1}, {"args":["--max-context","131072"],"layer_split":"auto"}, [(0,1000),(2,24000)], "key")
        self.assertEqual(config["gpu"], 2)
        self.assertNotIn("layer_split", config)

if __name__ == "__main__":
    unittest.main()
